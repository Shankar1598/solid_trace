package intake

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"strings"
)

type issueDetails struct {
	title       string
	culprit     string
	kind        string
	fingerprint string
}

func classifyIssue(payload map[string]interface{}) issueDetails {
	title := extractTitle(payload)
	culprit := extractCulprit(payload)
	kind := determineKind(payload)

	return issueDetails{
		title:       title,
		culprit:     culprit,
		kind:        kind,
		fingerprint: computeFingerprint(title, culprit, kind, extractCustomFingerprint(payload)),
	}
}

func extractCustomFingerprint(payload map[string]interface{}) []string {
	fingerprint, ok := payload["fingerprint"].([]interface{})
	if !ok {
		return nil
	}

	parts := make([]string, 0, len(fingerprint))
	for _, value := range fingerprint {
		if text, ok := value.(string); ok {
			parts = append(parts, text)
		}
	}

	return parts
}

// extractTitle is ported from the Ruby EventIngestor.
func extractTitle(payload map[string]interface{}) string {
	if exception, ok := payload["exception"]; ok {
		var values []interface{}

		switch v := exception.(type) {
		case map[string]interface{}:
			if valueList, ok := v["values"].([]interface{}); ok {
				values = valueList
			}
		case []interface{}:
			values = v
		}

		if len(values) > 0 {
			lastException, ok := values[len(values)-1].(map[string]interface{})
			if ok {
				typ, _ := lastException["type"].(string)
				value, _ := lastException["value"].(string)

				if strings.Contains(value, "\n") {
					value = strings.Split(value, "\n")[0]
				}

				if typ != "" && value != "" {
					return truncate(fmt.Sprintf("%s: %s", typ, value), 250)
				}
				if typ != "" {
					return typ
				}
				if value != "" {
					return value
				}
			}
		}
	}

	if message, ok := payload["message"].(string); ok && message != "" {
		return truncate(message, 250)
	}

	return "Unknown Error"
}

func extractCulprit(payload map[string]interface{}) string {
	if culprit, ok := payload["culprit"].(string); ok && culprit != "" {
		return culprit
	}
	if transaction, ok := payload["transaction"].(string); ok && transaction != "" {
		return transaction
	}

	return generateCulprit(payload)
}

func generateCulprit(payload map[string]interface{}) string {
	platform, _ := payload["platform"].(string)

	var exceptions []interface{}
	if exception, ok := payload["exception"]; ok {
		switch v := exception.(type) {
		case map[string]interface{}:
			if valueList, ok := v["values"].([]interface{}); ok {
				exceptions = valueList
			}
		case []interface{}:
			exceptions = v
		}
	}

	if len(exceptions) > 0 {
		lastException, ok := exceptions[len(exceptions)-1].(map[string]interface{})
		if ok {
			if mechanism, ok := lastException["mechanism"].(map[string]interface{}); ok {
				if synthetic, ok := mechanism["synthetic"].(bool); ok && synthetic {
					return ""
				}
			}
		}

		stacktraces := make([]interface{}, 0, len(exceptions))
		for _, exception := range exceptions {
			exceptionMap, ok := exception.(map[string]interface{})
			if !ok {
				continue
			}

			stacktrace, ok := exceptionMap["stacktrace"].(map[string]interface{})
			if !ok {
				continue
			}
			frames, ok := stacktrace["frames"].([]interface{})
			if ok && len(frames) > 0 {
				stacktraces = append(stacktraces, stacktrace)
			}
		}

		if len(stacktraces) > 0 {
			lastTrace, ok := stacktraces[len(stacktraces)-1].(map[string]interface{})
			if ok {
				return getStacktraceCulprit(lastTrace, platform)
			}
		}
	} else if stacktrace, ok := payload["stacktrace"].(map[string]interface{}); ok {
		if frames, ok := stacktrace["frames"].([]interface{}); ok && len(frames) > 0 {
			return getStacktraceCulprit(stacktrace, platform)
		}
	}

	if request, ok := payload["request"].(map[string]interface{}); ok {
		if url, ok := request["url"].(string); ok {
			return truncate(url, 250)
		}
	}

	return ""
}

func getStacktraceCulprit(stacktrace map[string]interface{}, platform string) string {
	frames, _ := stacktrace["frames"].([]interface{})
	var defaultCulprit string

	for idx := len(frames) - 1; idx >= 0; idx-- {
		frame, ok := frames[idx].(map[string]interface{})
		if !ok {
			continue
		}
		inApp, _ := frame["in_app"].(bool)

		culprit := getFrameCulprit(frame, platform)

		if inApp {
			if culprit != "" {
				return truncate(culprit, 250)
			}
		} else if defaultCulprit == "" {
			defaultCulprit = culprit
		}
	}

	if defaultCulprit != "" {
		return truncate(defaultCulprit, 250)
	}

	return ""
}

func getFrameCulprit(frame map[string]interface{}, platform string) string {
	framePlatform, _ := frame["platform"].(string)
	if framePlatform != "" {
		platform = framePlatform
	}

	function, _ := frame["function"].(string)
	filename, _ := frame["filename"].(string)
	lineNumber, _ := frame["lineno"].(float64)
	module, _ := frame["module"].(string)

	if platform == "objc" || platform == "cocoa" || platform == "native" {
		return function
	}

	fileLocation := filename
	if fileLocation != "" && lineNumber > 0 {
		fileLocation = fmt.Sprintf("%s:%.0f", filename, lineNumber)
	} else if fileLocation == "" {
		fileLocation = module
	}

	if fileLocation == "" {
		return ""
	}

	if platform == "javascript" || platform == "node" {
		if function != "" {
			return fmt.Sprintf("%s(%s)", function, fileLocation)
		}
		return fmt.Sprintf("?(%s)", fileLocation)
	}

	if function != "" {
		return fmt.Sprintf("%s in %s", function, fileLocation)
	}

	return fmt.Sprintf("? in %s", fileLocation)
}

func determineKind(payload map[string]interface{}) string {
	if _, ok := payload["csp-report"]; ok {
		return "csp"
	}
	if logger, ok := payload["logger"].(string); ok && logger == "csp" {
		return "csp"
	}
	if _, ok := payload["exception"]; ok {
		return "error"
	}
	return "default"
}

func computeFingerprint(title, culprit, kind string, customFingerprint []string) string {
	var input string

	if len(customFingerprint) > 0 {
		parts := make([]string, 0, len(customFingerprint))
		for _, part := range customFingerprint {
			if part == "{{ default }}" {
				parts = append(parts, fmt.Sprintf("%s||%s||%s", title, culprit, kind))
				continue
			}
			parts = append(parts, part)
		}
		input = strings.Join(parts, "||")
	} else {
		input = fmt.Sprintf("%s||%s||%s", title, culprit, kind)
	}

	hash := md5.Sum([]byte(input))
	return hex.EncodeToString(hash[:])
}

func truncate(value string, max int) string {
	if len(value) > max {
		return value[:max]
	}
	return value
}
