package logic

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"strings"
)

// ExtractTitle logic ported from Ruby EventIngestor
func ExtractTitle(payload map[string]interface{}) string {
	// Try exception first
	if exception, ok := payload["exception"]; ok {
		// Handle both dict and list structure
		var values []interface{}

		switch v := exception.(type) {
		case map[string]interface{}:
			if valList, ok := v["values"].([]interface{}); ok {
				values = valList
			}
		case []interface{}:
			values = v
		}

		if len(values) > 0 {
			lastEx := values[len(values)-1].(map[string]interface{})
			typ, _ := lastEx["type"].(string)
			val, _ := lastEx["value"].(string)

			// Truncate value to first line if needed? Ruby did `.split("\n").first`
			if strings.Contains(val, "\n") {
				val = strings.Split(val, "\n")[0]
			}

			if typ != "" && val != "" {
				title := fmt.Sprintf("%s: %s", typ, val)
				return truncate(title, 250)
			}
			if typ != "" {
				return typ
			}
			if val != "" {
				return val
			}
		}
	}

	// Fallback to message
	if msg, ok := payload["message"].(string); ok && msg != "" {
		return truncate(msg, 250)
	}

	return "Unknown Error"
}

func ExtractCulprit(payload map[string]interface{}) string {
	if culprit, ok := payload["culprit"].(string); ok && culprit != "" {
		return culprit
	}
	if txn, ok := payload["transaction"].(string); ok && txn != "" {
		return txn
	}

	return generateCulprit(payload)
}

func generateCulprit(payload map[string]interface{}) string {
	platform, _ := payload["platform"].(string)

	// Get exceptions values
	var exceptions []interface{}
	if exc, ok := payload["exception"]; ok {
		switch v := exc.(type) {
		case map[string]interface{}:
			if valList, ok := v["values"].([]interface{}); ok {
				exceptions = valList
			}
		case []interface{}:
			exceptions = v
		}
	}

	if len(exceptions) > 0 {
		lastEx := exceptions[len(exceptions)-1].(map[string]interface{})

		// Check mechanism synthetic
		if mechanism, ok := lastEx["mechanism"].(map[string]interface{}); ok {
			if synthetic, ok := mechanism["synthetic"].(bool); ok && synthetic {
				return ""
			}
		}

		// Get stacktraces
		var stacktraces []interface{}
		for _, e := range exceptions {
			em := e.(map[string]interface{})
			if st, ok := em["stacktrace"].(map[string]interface{}); ok {
				if frames, ok := st["frames"].([]interface{}); ok && len(frames) > 0 {
					stacktraces = append(stacktraces, st)
				}
			}
		}

		if len(stacktraces) > 0 {
			lastTrace := stacktraces[len(stacktraces)-1].(map[string]interface{})
			return getStacktraceCulprit(lastTrace, platform)
		}
	} else if st, ok := payload["stacktrace"].(map[string]interface{}); ok {
		// Bare stacktrace
		if frames, ok := st["frames"].([]interface{}); ok && len(frames) > 0 {
			return getStacktraceCulprit(st, platform)
		}
	}

	// Fallback to request URL
	if req, ok := payload["request"].(map[string]interface{}); ok {
		if url, ok := req["url"].(string); ok {
			return truncate(url, 250)
		}
	}

	return ""
}

func getStacktraceCulprit(stacktrace map[string]interface{}, platform string) string {
	frames, _ := stacktrace["frames"].([]interface{})
	var defaultCulprit string

	// Iterate reverse
	for i := len(frames) - 1; i >= 0; i-- {
		frame, _ := frames[i].(map[string]interface{})
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
	lineno, _ := frame["lineno"].(float64) // JSON numbers are floats
	module, _ := frame["module"].(string)

	if platform == "objc" || platform == "cocoa" || platform == "native" {
		return function
	}

	fileloc := filename
	if fileloc != "" && lineno > 0 {
		fileloc = fmt.Sprintf("%s:%.0f", filename, lineno)
	} else if fileloc == "" {
		fileloc = module
	}

	if fileloc == "" {
		return ""
	}

	if platform == "javascript" || platform == "node" {
		if function != "" {
			return fmt.Sprintf("%s(%s)", function, fileloc)
		}
		return fmt.Sprintf("?(%s)", fileloc)
	}

	if function != "" {
		return fmt.Sprintf("%s in %s", function, fileloc)
	}
	return fmt.Sprintf("? in %s", fileloc)
}

func DetermineKind(payload map[string]interface{}) string {
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

func ComputeFingerprint(title, culprit, kind string, customFingerprint []string) string {
	var input string

	if len(customFingerprint) > 0 {
		var parts []string
		for _, part := range customFingerprint {
			if part == "{{ default }}" {
				parts = append(parts, fmt.Sprintf("%s||%s||%s", title, culprit, kind))
			} else {
				parts = append(parts, part)
			}
		}
		input = strings.Join(parts, "||")
	} else {
		input = fmt.Sprintf("%s||%s||%s", title, culprit, kind)
	}

	hash := md5.Sum([]byte(input))
	return hex.EncodeToString(hash[:])
}

func truncate(s string, max int) string {
	if len(s) > max {
		return s[:max]
	}
	return s
}
