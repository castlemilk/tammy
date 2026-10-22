//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package localproduct

import (
	"context"
	"database/sql"
	"encoding/base32"
	"fmt"
	"testing"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/identity"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func realBASApprovalFactor(t *testing.T, server documentPageServer, auth *tammyv1.AuthenticationContext,
	userIndex int, purpose string,
) *tammyv1.FreshFactorContext {
	t.Helper()
	ctx := context.Background()
	base := uint64(0x8a00 + userIndex*4)
	enrolled, err := server.identity.EnrolTOTP(ctx, brandingRequest(&tammyv1.EnrolTOTPRequest{
		CommandContext: &tammyv1.CommandContext{Authentication: auth,
			IdempotencyKey: fmt.Sprintf("018f0000-0000-7000-8000-%012x", base)},
		CurrentPassword: &tammyv1.SecretInput{Utf8: []byte("purchase-role-password-long-enough")},
	}, server.capability))
	if err != nil || enrolled.Msg == nil || enrolled.Msg.Factor == nil || enrolled.Msg.ProvisioningSecret == nil {
		t.Fatalf("real TLS TOTP enrolment = %v, %v", enrolled, err)
	}
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(string(enrolled.Msg.ProvisioningSecret.Utf8))
	if err != nil {
		t.Fatal(err)
	}
	defer clear(secret)
	now := server.activation.Now()
	code := identity.TOTPCode(secret, now)
	if _, err := server.identity.ConfirmTOTP(ctx, brandingRequest(&tammyv1.ConfirmTOTPRequest{
		Authentication: auth, FactorId: enrolled.Msg.Factor.Id,
		Code: &tammyv1.TotpCodeInput{Value: code},
	}, server.capability)); err != nil {
		t.Fatalf("real TLS TOTP activation = %v", err)
	}
	// Confirmation consumed its time step. The next RFC 6238 step is within
	// the identity service's accepted adjacent-window bound.
	assertionCode := identity.TOTPCode(secret, now.Add(30*time.Second))
	asserted, err := server.identity.AssertTOTP(ctx, brandingRequest(&tammyv1.AssertTOTPRequest{
		Authentication: auth, Code: &tammyv1.TotpCodeInput{Value: assertionCode}, Purpose: purpose,
	}, server.capability))
	if err != nil || asserted.Msg == nil || asserted.Msg.FreshFactor == nil {
		t.Fatalf("real TLS purpose-bound assertion = %v, %v", asserted, err)
	}
	return proto.Clone(asserted.Msg.FreshFactor).(*tammyv1.FreshFactorContext)
}

func consumeBASFactorInCallerTransaction(ctx context.Context, server documentPageServer,
	auth *tammyv1.AuthenticationContext, factor *tammyv1.FreshFactorContext, purpose string, commit bool,
) error {
	tx, err := server.module.db.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	if err := server.activation.Identity.ConsumeFreshFactorWithin(ctx, tx, auth, factor, purpose); err != nil {
		_ = tx.Rollback()
		return err
	}
	if commit {
		return tx.Commit()
	}
	return tx.Rollback()
}

func basFactorConsumed(t *testing.T, server documentPageServer, assertionID string) int {
	t.Helper()
	var consumed int
	if err := server.module.db.QueryRowContext(context.Background(),
		`SELECT consumed FROM factor_assertions WHERE id = ?`, assertionID).Scan(&consumed); err != nil {
		t.Fatal(err)
	}
	return consumed
}

func TestBASApprovalFreshFactorIsPurposeBoundSingleUseAndTransactionOwned(t *testing.T) {
	server := newDocumentPageServer(t)
	ctx := context.Background()
	auth := server.userWithRole(t, tammyv1.Role_ROLE_BUSINESS_LODGER, 81)
	factor := realBASApprovalFactor(t, server, auth, 81, "bas_freeze_manual_handoff")
	if got := basFactorConsumed(t, server, factor.AssertionId); got != 0 {
		t.Fatalf("new assertion consumed=%d, want 0", got)
	}

	stale := proto.Clone(factor).(*tammyv1.FreshFactorContext)
	stale.AssertedAt = timestamppb.New(server.activation.Now().Add(-5 * time.Minute))
	if err := consumeBASFactorInCallerTransaction(ctx, server, auth, stale, "bas_freeze_manual_handoff", true); err == nil {
		t.Fatal("five-minute-old assertion marker was accepted")
	}
	if err := consumeBASFactorInCallerTransaction(ctx, server, auth, factor, "bas_replace_unused_attempt", true); err == nil {
		t.Fatal("assertion was accepted for a different approval purpose")
	}
	if got := basFactorConsumed(t, server, factor.AssertionId); got != 0 {
		t.Fatalf("stale or wrong-purpose attempt consumed assertion: %d", got)
	}

	// The production consumer saves its assertion and audit mutation through
	// this exact caller-owned SQLCipher tx. A later failure rolls both back.
	if err := consumeBASFactorInCallerTransaction(ctx, server, auth, factor, "bas_freeze_manual_handoff", false); err != nil {
		t.Fatalf("consume before injected caller failure: %v", err)
	}
	if got := basFactorConsumed(t, server, factor.AssertionId); got != 0 {
		t.Fatalf("rolled-back caller tx left assertion consumed: %d", got)
	}
	if err := consumeBASFactorInCallerTransaction(ctx, server, auth, factor, "bas_freeze_manual_handoff", true); err != nil {
		t.Fatalf("commit real factor consumption: %v", err)
	}
	if got := basFactorConsumed(t, server, factor.AssertionId); got != 1 {
		t.Fatalf("committed assertion consumed=%d, want 1", got)
	}
	// A second command using a new idempotency key still cannot reuse this
	// assertion; factor single-use is enforced by identity state, not request key.
	if err := consumeBASFactorInCallerTransaction(ctx, server, auth, factor, "bas_freeze_manual_handoff", true); err == nil {
		t.Fatal("consumed assertion was reused by a subsequent command")
	}
	if got := basFactorConsumed(t, server, factor.AssertionId); got != 1 {
		t.Fatalf("replay attempt changed consumed assertion to %d", got)
	}
}
