package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/modernpath/cli/internal/config"
	"github.com/modernpath/cli/internal/zitadel"
)

// refreshCredentialFn is the seam through which a near-expiry credential is
// renewed at the issuer; tests stub it, production calls zitadel.Refresh.
var refreshCredentialFn = func(ctx context.Context, profile zitadel.Profile, refreshToken string) (*zitadel.Token, error) {
	return zitadel.Refresh(ctx, profile, refreshToken)
}

const (
	// expiryWarnWindow is how long before expiry a store call starts acting:
	// refreshing, or warning so a long run can re-authenticate first.
	expiryWarnWindow = 10 * time.Minute
	// credentialLockWait bounds how long a contender waits for a refresh
	// another process is running; a lock older than the stale bound is a
	// crashed process's and is broken.
	credentialLockWait  = 5 * time.Second
	credentialLockStale = time.Minute
	refreshTimeout      = 30 * time.Second
)

var errCredentialLockBusy = errors.New("another process holds the credential lock")

// ensureFreshCredential is the pre-call expiry step every store verb runs
// through factoryEnvLoad (REQ-CROSS-389): inside the window it refreshes
// where the issuer allows, warns once per credential when it cannot, and
// refuses before any request once expired — naming the expiry and the
// remedy instead of letting the server answer a bare 401.
func ensureFreshCredential(env *factoryEnv, auth *config.Auth, now time.Time) (*config.Auth, error) {
	expiry := storedExpiry(auth)
	if expiry.IsZero() || expiry.Sub(now) > expiryWarnWindow {
		return auth, nil
	}
	repair := authRepairCommand(env.APIURL)

	var refreshErr error
	if profile, ok := refreshProfile(env.APIURL, auth.Issuer); ok && auth.RefreshToken != "" {
		fresh, err := refreshUnderLock(env.Root, auth, profile)
		if fresh != nil {
			auth = fresh
			expiry = storedExpiry(auth)
		}
		if err == nil || expiry.Sub(now) > expiryWarnWindow {
			return auth, nil
		}
		refreshErr = err
	}

	if !expiry.After(now) {
		return nil, expiredCredential(expiry, repair)
	}
	if noticeOnce(env.Root, "expiry-warning:"+credentialKey(auth.Token)) {
		why := ""
		if refreshErr != nil && !errors.Is(refreshErr, errCredentialLockBusy) {
			why = fmt.Sprintf("; refresh failed: %v", refreshErr)
		}
		printWarning("session expires in %s (at %s) — run '%s' before it does%s\n",
			expiry.Sub(now).Round(time.Second), expiry.Format(time.RFC3339), repair, why)
	}
	return auth, nil
}

// expiredCredential is the refusal for a token past its expiry, sent before
// any request so the server never answers it with a bare 401.
func expiredCredential(expiry time.Time, repair string) error {
	return credentialError{
		err:    fmt.Errorf("the session token expired at %s and the server will reject it — run '%s'", expiry.Format(time.RFC3339), repair),
		repair: repair,
	}
}

// storedExpiry is the credential's known expiry: the recorded expires_at,
// else the access token's exp claim, else zero.
func storedExpiry(auth *config.Auth) time.Time {
	claims, readable := zitadel.TokenClaims(auth.Token)
	return credentialExpiry(auth, claims, readable)
}

// refreshProfile resolves the issuer to refresh at: the baked-in profile of
// the API host, else the one the stored issuer names. A custom host with an
// unknown issuer cannot refresh and falls to the warning arm.
func refreshProfile(apiURL, issuer string) (zitadel.Profile, bool) {
	if p, ok := zitadel.ProfileForAPIURL(apiURL); ok {
		return p, true
	}
	return zitadel.ProfileForIssuer(issuer)
}

// refreshUnderLock renews the credential once for every process that meets
// the window at the same time: the winner refreshes and writes, a contender
// waits for the lock and re-reads what the winner wrote. The re-read file is
// returned even when the refresh itself failed, so the caller decides on the
// freshest credential there is. A contender never refreshes without the
// lock — ZITADEL rotates refresh tokens, and a stale write would drop the
// rotated one (F-CLI019-R2-02, R4-01).
func refreshUnderLock(root string, auth *config.Auth, profile zitadel.Profile) (*config.Auth, error) {
	release, ok := acquireCredentialLock(root)
	if !ok {
		reread, _ := config.ReadAuth()
		return reread, errCredentialLockBusy
	}
	defer release()

	if reread, err := config.ReadAuth(); err == nil && reread != nil && reread.Token != "" {
		auth = reread
		if storedExpiry(auth).Sub(time.Now()) > expiryWarnWindow {
			return auth, nil // the winner already refreshed
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), refreshTimeout)
	defer cancel()
	tok, err := refreshCredentialFn(ctx, profile, auth.RefreshToken)
	if err != nil {
		return auth, err
	}

	fresh := *auth
	fresh.Token = tok.AccessToken
	if tok.RefreshToken != "" {
		fresh.RefreshToken = tok.RefreshToken
	}
	fresh.ExpiresAt = ""
	if !tok.Expiry.IsZero() {
		fresh.ExpiresAt = tok.Expiry.UTC().Format(time.RFC3339)
	} else if c, ok := zitadel.TokenClaims(tok.AccessToken); ok && !c.ExpiresAt.IsZero() {
		fresh.ExpiresAt = c.ExpiresAt.Format(time.RFC3339)
	}
	if id, ok := zitadel.TokenClaims(tok.IDToken); ok && id.Email != "" {
		fresh.Actor = id.Email
	}
	if err := config.WriteAuth(&fresh); err != nil {
		return auth, fmt.Errorf("refreshed the session token but could not save it: %w", err)
	}
	return &fresh, nil
}

func credentialLockPath(root string) string {
	// The lock sits next to the credential it guards, which in a linked
	// worktree is the main checkout's (config.BindingDir).
	if dir, err := config.BindingDir(); err == nil && dir != "" {
		return filepath.Join(dir, "auth.lock")
	}
	return filepath.Join(root, config.ConfigDir, "auth.lock")
}

// acquireCredentialLock is the O_CREATE|O_EXCL lockfile the quiescent sync
// already uses, with a bounded wait instead of a skip.
func acquireCredentialLock(root string) (func(), bool) {
	path := credentialLockPath(root)
	deadline := time.Now().Add(credentialLockWait)
	for {
		if info, err := os.Stat(path); err == nil && time.Since(info.ModTime()) > credentialLockStale {
			_ = os.Remove(path)
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			fmt.Fprintf(f, "%d %s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339))
			_ = f.Close()
			return func() { _ = os.Remove(path) }, true
		}
		if time.Now().After(deadline) {
			return nil, false
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func credentialKey(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:6])
}
