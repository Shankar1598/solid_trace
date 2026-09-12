package handler

import (
	"crypto/subtle"

	"github.com/gofiber/fiber/v2"
)

const InternalTokenHeader = "X-SolidTrace-Internal-Token"

// InternalAuth allows a request only if it carries the shared internal token.
// An empty configured token rejects every request rather than matching an empty header.
func InternalAuth(token string) fiber.Handler {
	expected := []byte(token)

	return func(c *fiber.Ctx) error {
		given := []byte(c.Get(InternalTokenHeader))
		if len(expected) == 0 || subtle.ConstantTimeCompare(given, expected) != 1 {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
		}
		return c.Next()
	}
}
