package channel

import (
	"bytes"
	"context"
	"testing"
)

func TestA2A_SessionEstablishment(t *testing.T) {
	alice, err := NewA2AAdapter(A2AConfig{})
	if err != nil {
		t.Fatal(err)
	}
	bob, err := NewA2AAdapter(A2AConfig{})
	if err != nil {
		t.Fatal(err)
	}

	// Exchange public keys and establish sessions.
	err = alice.EstablishSession("bob", bob.PublicKeyHex(), "nonce-1")
	if err != nil {
		t.Fatalf("alice establish: %v", err)
	}
	err = bob.EstablishSession("alice", alice.PublicKeyHex(), "nonce-2")
	if err != nil {
		t.Fatalf("bob establish: %v", err)
	}
}

func TestA2A_EncryptDecrypt(t *testing.T) {
	alice, _ := NewA2AAdapter(A2AConfig{})
	bob, _ := NewA2AAdapter(A2AConfig{})

	_ = alice.EstablishSession("bob", bob.PublicKeyHex(), "nonce-1")
	_ = bob.EstablishSession("alice", alice.PublicKeyHex(), "nonce-2")

	// Alice encrypts.
	alice.mu.Lock()
	aliceSession := alice.sessions["bob"]
	alice.mu.Unlock()

	plaintext := []byte("hello from alice")
	ciphertext, err := alice.encrypt(aliceSession.sessionKey, plaintext)
	if err != nil {
		t.Fatal(err)
	}

	// Bob decrypts — sessions derive same key from ECDH.
	bob.mu.Lock()
	bobSession := bob.sessions["alice"]
	bob.mu.Unlock()

	decrypted, err := bob.decrypt(bobSession.sessionKey, ciphertext)
	if err != nil {
		t.Fatal(err)
	}
	if string(decrypted) != "hello from alice" {
		t.Errorf("decrypted %q, want 'hello from alice'", string(decrypted))
	}
}

func TestA2A_NonceReplay(t *testing.T) {
	alice, _ := NewA2AAdapter(A2AConfig{})
	bob, _ := NewA2AAdapter(A2AConfig{})

	err := alice.EstablishSession("bob", bob.PublicKeyHex(), "same-nonce")
	if err != nil {
		t.Fatal(err)
	}

	// Same nonce should be rejected.
	err = alice.EstablishSession("bob2", bob.PublicKeyHex(), "same-nonce")
	if err == nil {
		t.Fatal("expected nonce replay rejection")
	}
}

func TestA2A_RateLimit(t *testing.T) {
	cfg := A2AConfig{RateLimitPerPeer: 3}
	adapter, _ := NewA2AAdapter(cfg)
	bob, _ := NewA2AAdapter(A2AConfig{})

	for i := 0; i < 3; i++ {
		err := adapter.EstablishSession("bob", bob.PublicKeyHex(), "nonce-"+string(rune('a'+i)))
		if err != nil {
			t.Fatalf("request %d should succeed: %v", i, err)
		}
	}

	err := adapter.EstablishSession("bob", bob.PublicKeyHex(), "nonce-d")
	if err == nil {
		t.Fatal("expected rate limit rejection")
	}
}

func TestA2A_RecvSend(t *testing.T) {
	adapter, _ := NewA2AAdapter(A2AConfig{})

	// Nothing to receive initially.
	msg, err := adapter.Recv(context.Background())
	if err != nil || msg != nil {
		t.Fatal("expected nil recv on empty buffer")
	}
}

func TestA2A_DomainSalt_ByteOrdering(t *testing.T) {
	// Verify that domainSalt produces identical output regardless of
	// which side computes it — the fix uses bytes.Compare on raw key
	// bytes (matching Rust) rather than hex-string lexicographic order.
	alice, _ := NewA2AAdapter(A2AConfig{})
	bob, _ := NewA2AAdapter(A2AConfig{})

	saltFromAlice := alice.domainSalt(bob.publicKey)
	saltFromBob := bob.domainSalt(alice.publicKey)

	if !bytes.Equal(saltFromAlice, saltFromBob) {
		t.Fatalf("salt mismatch: alice=%x bob=%x", saltFromAlice, saltFromBob)
	}
}

func TestA2A_DomainSalt_DeterministicAcrossSessions(t *testing.T) {
	// Same key pair should always produce the same salt.
	alice, _ := NewA2AAdapter(A2AConfig{})
	bob, _ := NewA2AAdapter(A2AConfig{})

	salt1 := alice.domainSalt(bob.publicKey)
	salt2 := alice.domainSalt(bob.publicKey)

	if !bytes.Equal(salt1, salt2) {
		t.Fatal("salt not deterministic across calls")
	}
}

func TestA2A_SessionKey_Symmetric(t *testing.T) {
	// Both sides must derive the same session key for cross-decrypt to work.
	alice, _ := NewA2AAdapter(A2AConfig{})
	bob, _ := NewA2AAdapter(A2AConfig{})

	_ = alice.EstablishSession("bob", bob.PublicKeyHex(), "nonce-sym-1")
	_ = bob.EstablishSession("alice", alice.PublicKeyHex(), "nonce-sym-2")

	alice.mu.Lock()
	aliceKey := alice.sessions["bob"].sessionKey
	alice.mu.Unlock()

	bob.mu.Lock()
	bobKey := bob.sessions["alice"].sessionKey
	bob.mu.Unlock()

	if !bytes.Equal(aliceKey, bobKey) {
		t.Fatalf("session keys differ: alice=%x bob=%x", aliceKey, bobKey)
	}

	// Verify cross-decryption works end-to-end.
	plaintext := []byte("interop test payload")
	ct, err := alice.encrypt(aliceKey, plaintext)
	if err != nil {
		t.Fatal(err)
	}
	pt, err := bob.decrypt(bobKey, ct)
	if err != nil {
		t.Fatalf("cross-decrypt failed: %v", err)
	}
	if !bytes.Equal(pt, plaintext) {
		t.Fatalf("decrypted %q, want %q", pt, plaintext)
	}
}

func TestA2A_MaxMessageSize(t *testing.T) {
	alice, _ := NewA2AAdapter(A2AConfig{MaxMessageSize: 10})
	bob, _ := NewA2AAdapter(A2AConfig{})

	_ = alice.EstablishSession("bob", bob.PublicKeyHex(), "nonce-1")

	err := alice.Send(context.Background(), OutboundMessage{
		RecipientID: "bob",
		Content:     "this message is way too long for the limit",
	})
	if err == nil {
		t.Fatal("expected max size error")
	}
}
