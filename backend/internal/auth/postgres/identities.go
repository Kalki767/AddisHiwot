package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Kalki767/AddisHiwot/backend/internal/auth/domain"
	"github.com/Kalki767/AddisHiwot/backend/internal/db"
)

// The compiler checks that Identities keeps matching the domain interface.
var _ domain.IdentityRepository = (*Identities)(nil)

const providerGoogle = "google"

// Identities stores links between accounts and Google accounts.
type Identities struct {
	conn Conn
	q    *db.Queries
}

// NewIdentities returns an IdentityRepository on conn.
func NewIdentities(conn Conn) *Identities { return &Identities{conn: conn, q: db.New(conn)} }

// FindGoogleUser returns the account linked to a Google subject.
func (r *Identities) FindGoogleUser(ctx context.Context, subject string) (uuid.UUID, error) {
	id, err := r.q.GetIdentityUserID(ctx, db.GetIdentityUserIDParams{Provider: providerGoogle, Subject: subject})
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, domain.ErrNoIdentity
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("auth: find identity: %w", err)
	}
	return id, nil
}

// LinkGoogle links the Google account, marks the email verified and, when
// asked, drops the password and ends every session, in one transaction.
func (r *Identities) LinkGoogle(ctx context.Context, l domain.GoogleLink) error {
	err := inTx(ctx, r.conn, r.q, func(q *db.Queries) error {
		n, err := q.CreateIdentity(ctx, db.CreateIdentityParams{UserID: l.UserID, Provider: providerGoogle, Subject: l.Subject, CreatedAt: l.Now})
		if err != nil {
			return err
		}
		if n == 0 { // a link exists: fine only if it is this very one
			owner, err := q.GetIdentityUserID(ctx, db.GetIdentityUserIDParams{Provider: providerGoogle, Subject: l.Subject})
			if errors.Is(err, pgx.ErrNoRows) || (err == nil && owner != l.UserID) {
				return domain.ErrIdentityConflict
			}
			if err != nil {
				return err
			}
		}
		if l.DropPassword {
			if err := q.UpdatePasswordHash(ctx, db.UpdatePasswordHashParams{ID: l.UserID, PasswordHash: nil}); err != nil {
				return err
			}
			reason := string(domain.RevokeGoogleLinked)
			if _, err := q.RevokeUserSessions(ctx, db.RevokeUserSessionsParams{UserID: l.UserID, Reason: &reason, Now: &l.Now}); err != nil {
				return err
			}
		}
		return q.MarkEmailVerified(ctx, db.MarkEmailVerifiedParams{ID: l.UserID, VerifiedAt: &l.Now})
	})
	if err != nil && !errors.Is(err, domain.ErrIdentityConflict) {
		return fmt.Errorf("auth: link Google account: %w", err)
	}
	return err
}
