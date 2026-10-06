package middleware

import "github.com/gin-gonic/gin"

// NoStoreMiddleware prevents browsers and intermediary caches from retaining
// responses that can contain order credentials, payment state, or fulfillment.
func NoStoreMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store, private, max-age=0")
		c.Header("Pragma", "no-cache")
		c.Header("Expires", "0")
		c.Header("Referrer-Policy", "no-referrer")
		c.Next()
	}
}
