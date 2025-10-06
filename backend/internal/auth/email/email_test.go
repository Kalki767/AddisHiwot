package email

import (
	"context"
	"strings"
	"testing"

	"github.com/Kalki767/AddisHiwot/backend/internal/platform/mail"
)

type recorder struct{ sent []mail.Message }

func (r *recorder) Send(_ context.Context, m mail.Message) error {
	r.sent = append(r.sent, m)
	return nil
}

func TestVerificationCodeInBothLanguages(t *testing.T) {
	tests := map[string][]string{
		"en": {"Your verification code is 04217", "10 minutes"},
		"am": {"የማረጋገጫ ኮድዎ 04217 ነው", "ለ10 ደቂቃ"},
		"fr": {"Your verification code is 04217"}, // unknown language falls back to English
	}
	for locale, want := range tests {
		r := &recorder{}
		if err := New(r).VerificationCode(context.Background(), "k@example.com", "Kidist", locale, "04217"); err != nil {
			t.Fatal(err)
		}
		m := r.sent[0]
		if m.To != "k@example.com" {
			t.Errorf("%s: to = %q", locale, m.To)
		}
		for _, w := range want {
			if !strings.Contains(m.Text, w) {
				t.Errorf("%s: text lacks %q:\n%s", locale, w, m.Text)
			}
		}
	}
}

func TestSignupAttemptNoticeHasNoCode(t *testing.T) {
	for _, locale := range []string{"en", "am"} {
		r := &recorder{}
		if err := New(r).SignupAttempt(context.Background(), "k@example.com", "Kidist", locale); err != nil {
			t.Fatal(err)
		}
		if strings.ContainsAny(r.sent[0].Text, "0123456789") {
			t.Errorf("%s notice contains digits (a code?): %s", locale, r.sent[0].Text)
		}
	}
}

// Subjects show on lock screens: they must not name the app or its purpose.
func TestSubjectsAreNeutral(t *testing.T) {
	for locale, tx := range byLocale {
		for _, s := range []string{tx.codeSubject, tx.attemptSubject} {
			for _, word := range []string{"Addis", "Hiwot", "recovery", "አዲስ", "ሕይወት"} {
				if strings.Contains(s, word) {
					t.Errorf("%s subject %q names %q", locale, s, word)
				}
			}
		}
	}
}
