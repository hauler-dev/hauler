package registry

import "github.com/gofiber/fiber/v2"

func distributionHeadersMiddleware() fiber.Handler {
	return func(c *fiber.Ctx) (err error) {
		c.Set("Docker-Distribution-Api-Version", "registry/2.0")
		return c.Next()
	}
}