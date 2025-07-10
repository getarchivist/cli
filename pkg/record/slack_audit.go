//go:build slack_audit

package record

import "github.com/ohshell/cli/pkg/api"

func sendSlackAuditIfEnabled(trimmed string, cfg *sessionConfig) {
	if cfg != nil && cfg.slackAudit {
		go api.SendSlackAudit(trimmed, cfg.slackChannel, cfg.token, cfg.slackThreadTS)
	}
}
