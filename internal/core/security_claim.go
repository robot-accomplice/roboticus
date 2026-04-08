package core

// SecurityClaim is the immutable security principal resolved from all authentication
// layers. Constructed by claim resolvers (ResolveChannelClaim, ResolveAPIClaim,
// ResolveA2AClaim) — callers receive it but cannot modify the authority decision.
//
// This is the Go port of Rust's roboticus-core SecurityClaim. The composition
// algorithm is: effective = min(max(grants...), min(ceilings...)).
// Positive grants OR across layers (any layer can grant authority).
// Negative ceilings AND across layers (strictest restriction wins).
type SecurityClaim struct {
	// Authority is the resolved effective authority after claim composition.
	Authority AuthorityLevel `json:"authority"`
	// Sources identifies which authentication layer(s) contributed positive grants.
	Sources []ClaimSource `json:"sources"`
	// Ceiling is the effective ceiling applied by negative restrictions.
	Ceiling AuthorityLevel `json:"ceiling"`
	// ThreatDowngraded is true if the threat scanner applied a binding downgrade.
	ThreatDowngraded bool `json:"threat_downgraded"`
	// SenderID is the original sender identifier for audit trail.
	SenderID string `json:"sender_id"`
	// Channel is the channel that produced this claim.
	Channel string `json:"channel"`
}

// ClaimSource identifies which authentication layer contributed a positive grant.
type ClaimSource int

const (
	// ClaimSourceChannelAllowList means the sender passed the channel's allow-list.
	ClaimSourceChannelAllowList ClaimSource = iota
	// ClaimSourceTrustedSenderID means the sender matched channels.trusted_sender_ids.
	ClaimSourceTrustedSenderID
	// ClaimSourceAPIKey means HTTP API key authentication.
	ClaimSourceAPIKey
	// ClaimSourceWSTicket means WebSocket ticket authentication.
	ClaimSourceWSTicket
	// ClaimSourceA2ASession means A2A ECDH session key agreement.
	ClaimSourceA2ASession
	// ClaimSourceAnonymous means no authentication source — anonymous/default.
	ClaimSourceAnonymous
)

func (c ClaimSource) String() string {
	switch c {
	case ClaimSourceChannelAllowList:
		return "channel_allow_list"
	case ClaimSourceTrustedSenderID:
		return "trusted_sender_id"
	case ClaimSourceAPIKey:
		return "api_key"
	case ClaimSourceWSTicket:
		return "ws_ticket"
	case ClaimSourceA2ASession:
		return "a2a_session"
	case ClaimSourceAnonymous:
		return "anonymous"
	default:
		return "unknown"
	}
}

// ChannelClaimContext carries the inputs that a channel adapter knows about
// the sender, used by ResolveChannelClaim to derive a SecurityClaim.
type ChannelClaimContext struct {
	SenderID            string
	ChatID              string
	Channel             string
	SenderInAllowlist   bool
	AllowlistConfigured bool
	ThreatIsCaution     bool
	TrustedSenderIDs    []string
}

// ClaimSecurityConfig holds the configurable authority levels used by claim
// resolution. These are typically sourced from the SecurityConfig in config.go.
type ClaimSecurityConfig struct {
	AllowlistAuthority   AuthorityLevel
	TrustedAuthority     AuthorityLevel
	APIAuthority         AuthorityLevel
	ThreatCautionCeiling AuthorityLevel
}

// DefaultClaimSecurityConfig returns the default claim security configuration.
// Allowlist grants Peer, trusted grants Creator, API grants Creator, threat
// ceiling downgrades to External.
func DefaultClaimSecurityConfig() ClaimSecurityConfig {
	return ClaimSecurityConfig{
		AllowlistAuthority:   AuthorityPeer,
		TrustedAuthority:     AuthorityCreator,
		APIAuthority:         AuthorityCreator,
		ThreatCautionCeiling: AuthorityExternal,
	}
}

// ResolveChannelClaim resolves a SecurityClaim for a channel-originated message.
// This is the single authority resolution path for Telegram, Discord, WhatsApp,
// Signal, and Email messages.
func ResolveChannelClaim(ctx *ChannelClaimContext, sec ClaimSecurityConfig) SecurityClaim {
	var grants []AuthorityLevel
	var sources []ClaimSource
	var ceilings []AuthorityLevel

	// Positive grants (OR — any layer can grant)

	// Layer 1: Channel allow-list
	if ctx.AllowlistConfigured && ctx.SenderInAllowlist {
		grants = append(grants, sec.AllowlistAuthority)
		sources = append(sources, ClaimSourceChannelAllowList)
	}

	// Layer 2: trusted_sender_ids
	if len(ctx.TrustedSenderIDs) > 0 {
		for _, id := range ctx.TrustedSenderIDs {
			if id == ctx.ChatID || id == ctx.SenderID {
				grants = append(grants, sec.TrustedAuthority)
				sources = append(sources, ClaimSourceTrustedSenderID)
				break
			}
		}
	}

	// Negative ceilings (AND — strictest restriction wins)
	if ctx.ThreatIsCaution {
		ceilings = append(ceilings, sec.ThreatCautionCeiling)
	}

	return composeClaim(grants, sources, ceilings, ctx.SenderID, ctx.Channel)
}

// ResolveAPIClaim resolves a SecurityClaim for an HTTP API or WebSocket request.
func ResolveAPIClaim(threatIsCaution bool, channel string, sec ClaimSecurityConfig) SecurityClaim {
	grants := []AuthorityLevel{sec.APIAuthority}
	sources := []ClaimSource{ClaimSourceAPIKey}
	var ceilings []AuthorityLevel

	if threatIsCaution {
		ceilings = append(ceilings, sec.ThreatCautionCeiling)
	}

	return composeClaim(grants, sources, ceilings, "api", channel)
}

// ResolveA2AClaim resolves a SecurityClaim for an A2A (agent-to-agent) session.
// A2A peers receive Peer authority — never Creator.
func ResolveA2AClaim(threatIsCaution bool, senderID string, sec ClaimSecurityConfig) SecurityClaim {
	grants := []AuthorityLevel{AuthorityPeer}
	sources := []ClaimSource{ClaimSourceA2ASession}
	var ceilings []AuthorityLevel

	if threatIsCaution {
		ceilings = append(ceilings, sec.ThreatCautionCeiling)
	}

	return composeClaim(grants, sources, ceilings, senderID, "a2a")
}

// composeClaim applies the claim composition algorithm:
// effective = min(max(grants...), min(ceilings...))
func composeClaim(
	grants []AuthorityLevel,
	sources []ClaimSource,
	ceilings []AuthorityLevel,
	senderID, channel string,
) SecurityClaim {
	// Best grant (OR — any layer can grant)
	effectiveGrant := AuthorityExternal
	if len(grants) == 0 {
		sources = append(sources, ClaimSourceAnonymous)
	} else {
		effectiveGrant = maxAuthority(grants...)
	}

	// Strictest ceiling (AND — all must allow)
	effectiveCeiling := AuthorityCreator // no restrictions by default
	if len(ceilings) > 0 {
		effectiveCeiling = minAuthority(ceilings...)
	}

	finalAuthority := minAuthority(effectiveGrant, effectiveCeiling)
	threatDowngraded := len(ceilings) > 0 && finalAuthority < effectiveGrant

	return SecurityClaim{
		Authority:        finalAuthority,
		Sources:          sources,
		Ceiling:          effectiveCeiling,
		ThreatDowngraded: threatDowngraded,
		SenderID:         senderID,
		Channel:          channel,
	}
}

// maxAuthority returns the highest AuthorityLevel from the given values.
// Uses the iota ordering: External(0) < Peer(1) < SelfGenerated(2) < Creator(3).
func maxAuthority(levels ...AuthorityLevel) AuthorityLevel {
	best := levels[0]
	for _, l := range levels[1:] {
		if l > best {
			best = l
		}
	}
	return best
}

// minAuthority returns the lowest AuthorityLevel from the given values.
func minAuthority(levels ...AuthorityLevel) AuthorityLevel {
	worst := levels[0]
	for _, l := range levels[1:] {
		if l < worst {
			worst = l
		}
	}
	return worst
}
