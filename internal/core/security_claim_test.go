package core

import "testing"

func TestComposeClaim_NoGrants_YieldsExternal(t *testing.T) {
	sec := DefaultClaimSecurityConfig()
	ctx := &ChannelClaimContext{
		SenderID: "u1", ChatID: "c1", Channel: "telegram",
		SenderInAllowlist: false, AllowlistConfigured: true,
		ThreatIsCaution: false, TrustedSenderIDs: nil,
	}
	claim := ResolveChannelClaim(ctx, sec)
	if claim.Authority != AuthorityExternal {
		t.Errorf("expected External, got %v", claim.Authority)
	}
	if !containsSource(claim.Sources, ClaimSourceAnonymous) {
		t.Error("expected Anonymous source")
	}
}

func TestComposeClaim_AllowlistOnly_YieldsPeer(t *testing.T) {
	sec := DefaultClaimSecurityConfig()
	ctx := &ChannelClaimContext{
		SenderID: "u1", ChatID: "c1", Channel: "telegram",
		SenderInAllowlist: true, AllowlistConfigured: true,
		ThreatIsCaution: false, TrustedSenderIDs: nil,
	}
	claim := ResolveChannelClaim(ctx, sec)
	if claim.Authority != AuthorityPeer {
		t.Errorf("expected Peer, got %v", claim.Authority)
	}
	if !containsSource(claim.Sources, ClaimSourceChannelAllowList) {
		t.Error("expected ChannelAllowList source")
	}
}

func TestComposeClaim_TrustedOnly_YieldsCreator(t *testing.T) {
	sec := DefaultClaimSecurityConfig()
	ctx := &ChannelClaimContext{
		SenderID: "u1", ChatID: "c1", Channel: "telegram",
		SenderInAllowlist: false, AllowlistConfigured: true,
		ThreatIsCaution: false, TrustedSenderIDs: []string{"u1"},
	}
	claim := ResolveChannelClaim(ctx, sec)
	if claim.Authority != AuthorityCreator {
		t.Errorf("expected Creator, got %v", claim.Authority)
	}
}

func TestComposeClaim_TrustedByChatID(t *testing.T) {
	sec := DefaultClaimSecurityConfig()
	ctx := &ChannelClaimContext{
		SenderID: "u1", ChatID: "c1", Channel: "telegram",
		SenderInAllowlist: false, AllowlistConfigured: true,
		ThreatIsCaution: false, TrustedSenderIDs: []string{"c1"},
	}
	claim := ResolveChannelClaim(ctx, sec)
	if claim.Authority != AuthorityCreator {
		t.Errorf("expected Creator, got %v", claim.Authority)
	}
}

func TestComposeClaim_BothGrantsYieldsCreator(t *testing.T) {
	sec := DefaultClaimSecurityConfig()
	ctx := &ChannelClaimContext{
		SenderID: "u1", ChatID: "c1", Channel: "telegram",
		SenderInAllowlist: true, AllowlistConfigured: true,
		ThreatIsCaution: false, TrustedSenderIDs: []string{"u1"},
	}
	claim := ResolveChannelClaim(ctx, sec)
	if claim.Authority != AuthorityCreator {
		t.Errorf("expected Creator (max of Peer, Creator), got %v", claim.Authority)
	}
	if !containsSource(claim.Sources, ClaimSourceChannelAllowList) {
		t.Error("expected ChannelAllowList source")
	}
	if !containsSource(claim.Sources, ClaimSourceTrustedSenderID) {
		t.Error("expected TrustedSenderID source")
	}
}

func TestComposeClaim_ThreatCeilingDowngradesCreator(t *testing.T) {
	sec := DefaultClaimSecurityConfig()
	ctx := &ChannelClaimContext{
		SenderID: "u1", ChatID: "c1", Channel: "telegram",
		SenderInAllowlist: true, AllowlistConfigured: true,
		ThreatIsCaution: true, TrustedSenderIDs: []string{"u1"},
	}
	claim := ResolveChannelClaim(ctx, sec)
	if claim.Authority != AuthorityExternal {
		t.Errorf("expected External (Creator capped by External ceiling), got %v", claim.Authority)
	}
	if !claim.ThreatDowngraded {
		t.Error("expected ThreatDowngraded=true")
	}
	if claim.Ceiling != AuthorityExternal {
		t.Errorf("expected ceiling External, got %v", claim.Ceiling)
	}
}

func TestComposeClaim_CustomThreatCeiling(t *testing.T) {
	sec := DefaultClaimSecurityConfig()
	sec.ThreatCautionCeiling = AuthorityPeer
	ctx := &ChannelClaimContext{
		SenderID: "u1", ChatID: "c1", Channel: "telegram",
		SenderInAllowlist: true, AllowlistConfigured: true,
		ThreatIsCaution: true, TrustedSenderIDs: []string{"u1"},
	}
	claim := ResolveChannelClaim(ctx, sec)
	if claim.Authority != AuthorityPeer {
		t.Errorf("expected Peer (Creator capped by Peer ceiling), got %v", claim.Authority)
	}
	if !claim.ThreatDowngraded {
		t.Error("expected ThreatDowngraded=true")
	}
}

func TestComposeClaim_NoThreatNoCeiling(t *testing.T) {
	sec := DefaultClaimSecurityConfig()
	ctx := &ChannelClaimContext{
		SenderID: "u1", ChatID: "c1", Channel: "telegram",
		SenderInAllowlist: true, AllowlistConfigured: true,
		ThreatIsCaution: false, TrustedSenderIDs: []string{"u1"},
	}
	claim := ResolveChannelClaim(ctx, sec)
	if claim.Authority != AuthorityCreator {
		t.Errorf("expected Creator, got %v", claim.Authority)
	}
	if claim.ThreatDowngraded {
		t.Error("expected ThreatDowngraded=false")
	}
	if claim.Ceiling != AuthorityCreator {
		t.Errorf("expected ceiling Creator (no restrictions), got %v", claim.Ceiling)
	}
}

func TestComposeClaim_EmptyAllowlist_RejectsToExternal(t *testing.T) {
	sec := DefaultClaimSecurityConfig()
	ctx := &ChannelClaimContext{
		SenderID: "u1", ChatID: "c1", Channel: "telegram",
		SenderInAllowlist: false, AllowlistConfigured: false,
		ThreatIsCaution: false, TrustedSenderIDs: nil,
	}
	claim := ResolveChannelClaim(ctx, sec)
	if claim.Authority != AuthorityExternal {
		t.Errorf("expected External, got %v", claim.Authority)
	}
	if !containsSource(claim.Sources, ClaimSourceAnonymous) {
		t.Error("expected Anonymous source")
	}
}

func TestResolveAPIClaim_DefaultCreator(t *testing.T) {
	sec := DefaultClaimSecurityConfig()
	claim := ResolveAPIClaim(false, "api", sec)
	if claim.Authority != AuthorityCreator {
		t.Errorf("expected Creator, got %v", claim.Authority)
	}
	if !containsSource(claim.Sources, ClaimSourceAPIKey) {
		t.Error("expected APIKey source")
	}
}

func TestResolveAPIClaim_ThreatDowngrade(t *testing.T) {
	sec := DefaultClaimSecurityConfig()
	claim := ResolveAPIClaim(true, "api", sec)
	if claim.Authority != AuthorityExternal {
		t.Errorf("expected External, got %v", claim.Authority)
	}
	if !claim.ThreatDowngraded {
		t.Error("expected ThreatDowngraded=true")
	}
}

func TestResolveA2AClaim_AlwaysPeer(t *testing.T) {
	sec := DefaultClaimSecurityConfig()
	claim := ResolveA2AClaim(false, "peer-agent", sec)
	if claim.Authority != AuthorityPeer {
		t.Errorf("expected Peer, got %v", claim.Authority)
	}
	if !containsSource(claim.Sources, ClaimSourceA2ASession) {
		t.Error("expected A2ASession source")
	}
}

func TestResolveA2AClaim_ThreatDowngrade(t *testing.T) {
	sec := DefaultClaimSecurityConfig()
	claim := ResolveA2AClaim(true, "peer-agent", sec)
	if claim.Authority != AuthorityExternal {
		t.Errorf("expected External, got %v", claim.Authority)
	}
	if !claim.ThreatDowngraded {
		t.Error("expected ThreatDowngraded=true")
	}
}

func TestComposeClaim_ThreatPresentButNotBinding(t *testing.T) {
	// External user with no grants + threat_is_caution = true.
	// Ceiling is External, grant is External → ceiling is not binding.
	sec := DefaultClaimSecurityConfig()
	ctx := &ChannelClaimContext{
		SenderID: "unknown", ChatID: "c1", Channel: "telegram",
		SenderInAllowlist: false, AllowlistConfigured: true,
		ThreatIsCaution: true, TrustedSenderIDs: nil,
	}
	claim := ResolveChannelClaim(ctx, sec)
	if claim.Authority != AuthorityExternal {
		t.Errorf("expected External, got %v", claim.Authority)
	}
	if claim.ThreatDowngraded {
		t.Error("expected ThreatDowngraded=false (ceiling not binding)")
	}
}

func TestComposeClaim_Monotonicity_AddingGrantNeverDecreases(t *testing.T) {
	sec := DefaultClaimSecurityConfig()
	// Without trusted
	ctx1 := &ChannelClaimContext{
		SenderID: "u1", ChatID: "c1", Channel: "telegram",
		SenderInAllowlist: true, AllowlistConfigured: true,
		ThreatIsCaution: false, TrustedSenderIDs: nil,
	}
	claim1 := ResolveChannelClaim(ctx1, sec)

	// With trusted (additional grant)
	ctx2 := &ChannelClaimContext{
		SenderID: "u1", ChatID: "c1", Channel: "telegram",
		SenderInAllowlist: true, AllowlistConfigured: true,
		ThreatIsCaution: false, TrustedSenderIDs: []string{"u1"},
	}
	claim2 := ResolveChannelClaim(ctx2, sec)

	if claim2.Authority < claim1.Authority {
		t.Errorf("adding grant should never decrease: %v < %v", claim2.Authority, claim1.Authority)
	}
}

func TestComposeClaim_Monotonicity_AddingCeilingNeverIncreases(t *testing.T) {
	sec := DefaultClaimSecurityConfig()
	trusted := []string{"u1"}
	// Without threat
	ctx1 := &ChannelClaimContext{
		SenderID: "u1", ChatID: "c1", Channel: "telegram",
		SenderInAllowlist: true, AllowlistConfigured: true,
		ThreatIsCaution: false, TrustedSenderIDs: trusted,
	}
	claim1 := ResolveChannelClaim(ctx1, sec)

	// With threat (additional ceiling)
	ctx2 := &ChannelClaimContext{
		SenderID: "u1", ChatID: "c1", Channel: "telegram",
		SenderInAllowlist: true, AllowlistConfigured: true,
		ThreatIsCaution: true, TrustedSenderIDs: trusted,
	}
	claim2 := ResolveChannelClaim(ctx2, sec)

	if claim2.Authority > claim1.Authority {
		t.Errorf("adding ceiling should never increase: %v > %v", claim2.Authority, claim1.Authority)
	}
}

func TestComposeClaim_CustomAllowlistAuthority(t *testing.T) {
	sec := DefaultClaimSecurityConfig()
	sec.AllowlistAuthority = AuthorityCreator
	ctx := &ChannelClaimContext{
		SenderID: "u1", ChatID: "c1", Channel: "telegram",
		SenderInAllowlist: true, AllowlistConfigured: true,
		ThreatIsCaution: false, TrustedSenderIDs: nil,
	}
	claim := ResolveChannelClaim(ctx, sec)
	if claim.Authority != AuthorityCreator {
		t.Errorf("expected Creator, got %v", claim.Authority)
	}
}

func TestComposeClaim_CustomAPIAuthorityDowngraded(t *testing.T) {
	sec := DefaultClaimSecurityConfig()
	sec.APIAuthority = AuthorityPeer
	claim := ResolveAPIClaim(false, "api", sec)
	if claim.Authority != AuthorityPeer {
		t.Errorf("expected Peer, got %v", claim.Authority)
	}
}

func containsSource(sources []ClaimSource, target ClaimSource) bool {
	for _, s := range sources {
		if s == target {
			return true
		}
	}
	return false
}
