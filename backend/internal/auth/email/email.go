// Package email writes the auth emails in English and Amharic and hands them to
// a mail sender. It implements the auth domain's Notifier.
//
// Subjects are deliberately neutral: they show on phone lock screens, where
// "Addis Hiwot" or "recovery" could tell an onlooker more than the person wants.
package email

import (
	"bytes"
	"context"
	"fmt"
	"text/template"

	"github.com/Kalki767/AddisHiwot/backend/internal/auth/domain"
	"github.com/Kalki767/AddisHiwot/backend/internal/platform/mail"
)

// The compiler checks that Notifier keeps matching the domain interface.
var _ domain.Notifier = (*Notifier)(nil)

type texts struct {
	codeSubject, code, attemptSubject, attempt string
}

// The Amharic texts are drafts to be checked by an Amharic speaker.
var byLocale = map[string]texts{
	"en": {
		codeSubject: "Your verification code",
		code: `Hello {{.Name}},

Your verification code is {{.Code}}. It works for {{.Minutes}} minutes.

If you did not ask for this code, you can ignore this email.

Addis Hiwot`,
		attemptSubject: "Sign-up attempt with your email",
		attempt: `Hello {{.Name}},

Someone tried to create an account with this email address. You already have an
account, so nothing was changed.

If this was you, log in, or use "Forgot password?" if you do not remember your
password. If it was not you, you can ignore this email.

Addis Hiwot`,
	},
	"am": {
		codeSubject: "የማረጋገጫ ኮድዎ",
		code: `ሰላም {{.Name}}፣

የማረጋገጫ ኮድዎ {{.Code}} ነው። ለ{{.Minutes}} ደቂቃ ያገለግላል።

ይህን ኮድ ካልጠየቁ ይህን ኢሜይል ችላ ማለት ይችላሉ።

አዲስ ሕይወት`,
		attemptSubject: "በኢሜይልዎ የመመዝገብ ሙከራ",
		attempt: `ሰላም {{.Name}}፣

አንድ ሰው በዚህ የኢሜይል አድራሻ አካውንት ለመክፈት ሞክሯል። አካውንት ስላለዎት ምንም አልተለወጠም።

እርስዎ ከሆኑ ይግቡ፤ የይለፍ ቃልዎን ካላስታወሱ "የይለፍ ቃል ረሱ?" የሚለውን ይጠቀሙ። እርስዎ ካልሆኑ ይህን ኢሜይል ችላ ማለት ይችላሉ።

አዲስ ሕይወት`,
	},
}

// Notifier sends the auth emails through a mail sender.
type Notifier struct {
	sender mail.Sender
}

// New returns a Notifier that sends through sender.
func New(sender mail.Sender) *Notifier { return &Notifier{sender: sender} }

// VerificationCode emails a verification code.
func (n *Notifier) VerificationCode(ctx context.Context, to, name, locale, code string) error {
	t := textsFor(locale)
	return n.send(ctx, to, t.codeSubject, t.code, map[string]any{"Name": name, "Code": code, "Minutes": int(domain.CodeTTL.Minutes())})
}

// SignupAttempt tells an account owner someone tried to sign up with their email.
func (n *Notifier) SignupAttempt(ctx context.Context, to, name, locale string) error {
	t := textsFor(locale)
	return n.send(ctx, to, t.attemptSubject, t.attempt, map[string]any{"Name": name})
}

func (n *Notifier) send(ctx context.Context, to, subject, body string, data map[string]any) error {
	text, err := render(body, data)
	if err != nil {
		return err
	}
	return n.sender.Send(ctx, mail.Message{To: to, Subject: subject, Text: text})
}

func textsFor(locale string) texts {
	if t, ok := byLocale[locale]; ok {
		return t
	}
	return byLocale["en"]
}

func render(body string, data map[string]any) (string, error) {
	tmpl, err := template.New("email").Option("missingkey=error").Parse(body)
	if err != nil {
		return "", fmt.Errorf("email: template: %w", err)
	}
	var b bytes.Buffer
	if err := tmpl.Execute(&b, data); err != nil {
		return "", fmt.Errorf("email: render: %w", err)
	}
	return b.String(), nil
}
