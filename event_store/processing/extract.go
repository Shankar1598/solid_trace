package processing

import "time"

// extractTimestamp reads the event's "timestamp", which Sentry allows as
// either Unix seconds or an RFC 3339 string (UTC when it has no offset).
// Without a usable one, the Event's receive time is its timestamp.
func extractTimestamp(payload map[string]interface{}, receivedAt time.Time) time.Time {
	switch timestamp := payload["timestamp"].(type) {
	case float64:
		return time.UnixMilli(int64(timestamp * 1000))
	case string:
		if parsed, err := time.Parse(time.RFC3339Nano, timestamp); err == nil {
			return parsed
		}
		if parsed, err := time.Parse("2006-01-02T15:04:05.999999999", timestamp); err == nil {
			return parsed
		}
	}
	return receivedAt
}

func extractTags(payload map[string]interface{}) map[string]string {
	tags := make(map[string]string)
	rawTags, ok := payload["tags"].(map[string]interface{})
	if !ok {
		return tags
	}

	for key, value := range rawTags {
		if text, ok := value.(string); ok {
			tags[key] = text
		}
	}

	return tags
}

func extractString(payload map[string]interface{}, key string) string {
	value, ok := payload[key]
	if !ok {
		return ""
	}

	text, _ := value.(string)
	return text
}
