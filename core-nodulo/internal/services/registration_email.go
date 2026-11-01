package services

import (
	"bytes"
	"fmt"
	htmltemplate "html/template"
	"net/url"
	"strings"
	texttemplate "text/template"
)

type registrationEmailContent struct {
	Name            string
	Approved        bool
	AppURL          string
	RejectionReason string
}

var registrationHTMLTemplate = htmltemplate.Must(htmltemplate.New("registration-email").Parse(`<!doctype html>
<html lang="pt-BR">
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><meta name="color-scheme" content="light"><title>{{if .Approved}}Cadastro aprovado{{else}}Atualização do cadastro{{end}}</title></head>
<body style="margin:0;padding:0;background:#f2f5f2;font-family:Inter,-apple-system,BlinkMacSystemFont,'Segoe UI',Arial,sans-serif;color:#202721;">
  <table role="presentation" width="100%" cellspacing="0" cellpadding="0" style="background:#f2f5f2;padding:36px 14px;">
    <tr><td align="center">
      <table role="presentation" width="100%" cellspacing="0" cellpadding="0" style="max-width:580px;background:#ffffff;border:1px solid #e3e9e3;border-radius:18px;overflow:hidden;">
        <tr><td style="padding:24px 32px;border-bottom:1px solid #edf0ed;">
          <table role="presentation" cellspacing="0" cellpadding="0"><tr>
            <td width="38" height="38" align="center" valign="middle" style="width:38px;height:38px;border-radius:11px;background:#eaf3ec;color:#386345;font-size:18px;font-weight:700;">N</td>
            <td style="padding-left:11px;color:#26352a;font-size:15px;font-weight:700;letter-spacing:.04em;">NÓDULO</td>
          </tr></table>
        </td></tr>
        <tr><td style="padding:36px 32px 30px;">
          <p style="margin:0 0 12px;color:{{if .Approved}}#397449{{else}}#8b5c27{{end}};font-size:11px;font-weight:700;letter-spacing:.13em;text-transform:uppercase;">{{if .Approved}}ACESSO LIBERADO{{else}}ATUALIZAÇÃO DO CADASTRO{{end}}</p>
          <h1 style="margin:0;color:#202721;font-size:27px;font-weight:650;letter-spacing:-.035em;line-height:1.25;">{{if .Approved}}Seu cadastro foi aprovado{{else}}Uma decisão foi tomada{{end}}</h1>
          <p style="margin:22px 0 0;color:#374139;font-size:15px;line-height:1.7;">{{if .Name}}Olá, {{.Name}}.{{else}}Olá.{{end}}</p>
          {{if .Approved}}
          <p style="margin:10px 0 0;color:#58635a;font-size:15px;line-height:1.7;">A administração aprovou seu cadastro. Sua conta está pronta e você já pode acessar a plataforma.</p>
          {{else}}
          <p style="margin:10px 0 0;color:#58635a;font-size:15px;line-height:1.7;">A administração analisou sua solicitação e, desta vez, não aprovou o cadastro.</p>
          {{if .RejectionReason}}<div style="margin-top:18px;border:1px solid #f0e4d2;border-radius:11px;background:#fcf8f1;padding:15px 17px;"><p style="margin:0 0 5px;color:#765426;font-size:12px;font-weight:650;">Justificativa</p><p style="margin:0;color:#554b3d;font-size:14px;line-height:1.65;white-space:pre-wrap;">{{.RejectionReason}}</p></div>{{end}}
          {{end}}
          {{if .AppURL}}
          <table role="presentation" cellspacing="0" cellpadding="0" style="margin-top:27px;"><tr><td align="center" style="border-radius:10px;background:#477552;">
            <a href="{{.AppURL}}" style="display:inline-block;padding:13px 20px;color:#ffffff;font-size:14px;font-weight:650;text-decoration:none;">{{if .Approved}}Acessar o NÓDULO{{else}}Abrir o NÓDULO{{end}}</a>
          </td></tr></table>
          {{end}}
          <p style="margin:27px 0 0;color:#758077;font-size:12px;line-height:1.65;">O NÓDULO é uma plataforma open source voltada à autonomia e à soberania sobre dados. As regras de acesso e operação são definidas por cada instalação.</p>
        </td></tr>
        <tr><td style="padding:18px 32px;background:#f8faf8;border-top:1px solid #edf0ed;color:#899189;font-size:11px;line-height:1.6;">Esta mensagem foi enviada porque houve uma solicitação de cadastro no NÓDULO. Se você não reconhece essa solicitação, pode ignorar este email.</td></tr>
      </table>
      <p style="margin:16px 0 0;color:#9aa29b;font-size:11px;">NÓDULO · Inteligência artificial sob seu controle</p>
    </td></tr>
  </table>
</body>
</html>`))

var registrationTextTemplate = texttemplate.Must(texttemplate.New("registration-text").Parse(`NÓDULO — {{if .Approved}}Cadastro aprovado{{else}}Atualização do cadastro{{end}}

{{if .Name}}Olá, {{.Name}}.{{else}}Olá.{{end}}

{{if .Approved}}A administração aprovou seu cadastro. Sua conta está pronta e você já pode acessar a plataforma.{{else}}A administração analisou sua solicitação e, desta vez, não aprovou o cadastro.{{end}}
{{if and (not .Approved) .RejectionReason}}
Justificativa: {{.RejectionReason}}
{{end}}{{if .AppURL}}
Acesse: {{.AppURL}}
{{end}}
O NÓDULO é uma plataforma open source voltada à autonomia e à soberania sobre dados. As regras de acesso e operação são definidas por cada instalação.

Se você não reconhece esta solicitação de cadastro, ignore este email.
`))

func renderRegistrationEmail(name, status, reason, appURL string) (subject, plainText, htmlBody string, err error) {
	approved := status == "approved"
	data := registrationEmailContent{
		Name:            safeMailText(strings.TrimSpace(name)),
		Approved:        approved,
		RejectionReason: safeMailText(strings.TrimSpace(reason)),
		AppURL:          strings.TrimRight(strings.TrimSpace(appURL), "/"),
	}
	if data.AppURL != "" {
		parsed, parseErr := url.Parse(data.AppURL)
		if parseErr != nil || parsed.Host == "" || parsed.User != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.RawQuery != "" || parsed.Fragment != "" {
			return "", "", "", fmt.Errorf("APP_PUBLIC_URL deve ser uma URL HTTP(S) válida sem credenciais")
		}
	}
	var plain bytes.Buffer
	if err := registrationTextTemplate.Execute(&plain, data); err != nil {
		return "", "", "", fmt.Errorf("render plain registration email: %w", err)
	}
	var html bytes.Buffer
	if err := registrationHTMLTemplate.Execute(&html, data); err != nil {
		return "", "", "", fmt.Errorf("render HTML registration email: %w", err)
	}
	if approved {
		subject = "Seu acesso ao NÓDULO foi aprovado"
	} else {
		subject = "Atualização sobre seu cadastro no NÓDULO"
	}
	return subject, plain.String(), html.String(), nil
}
