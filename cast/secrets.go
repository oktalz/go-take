package cast

import "strings"

// SecretPattern is a regular expression for common credentials: tokens of
// well-known services, JSON web tokens, and the value after keys such as
// password= or api_key: (its "secret" group). Used with ReplaceOptions.Mask
// it hides them and keeps the layout.
var SecretPattern = strings.Join([]string{
	`gh[pousr]_[A-Za-z0-9]{36,}`,                                      // GitHub
	`github_pat_[A-Za-z0-9_]{40,}`,                                    // GitHub fine-grained
	`glpat-[A-Za-z0-9_-]{20,}`,                                        // GitLab
	`\b(?:AKIA|ASIA)[A-Z0-9]{16}\b`,                                   // AWS access key id
	`xox[abposr]-[A-Za-z0-9-]{10,}`,                                   // Slack
	`\bsk-[A-Za-z0-9_-]{20,}`,                                         // OpenAI, Anthropic and similar API keys
	`AIza[0-9A-Za-z_-]{35}`,                                           // Google API key
	`\b[rs]k_(?:live|test)_[0-9A-Za-z]{20,}`,                          // Stripe
	`\beyJ[A-Za-z0-9_-]{8,}\.eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`, // JWT
	`(?i:password|passwd|secret|token|api[_-]?key|access[_-]?key)["']?\s*[=:]\s*["']?(?P<secret>[^\s"';]{4,})`,
}, "|")
