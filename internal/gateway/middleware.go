package gateway

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"golang.org/x/time/rate"
)

// RecoveryMiddleware 恢复中间件
func RecoveryMiddleware(logger *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				logger.Error("Panic recovered",
					zap.Any("error", err),
					zap.String("path", c.Request.URL.Path),
					zap.String("method", c.Request.Method),
				)

				c.JSON(http.StatusInternalServerError, gin.H{
					"error": "Internal server error",
				})
				c.Abort()
			}
		}()

		c.Next()
	}
}

// LoggingMiddleware 日志中间件
func LoggingMiddleware(logger *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery

		c.Next()

		end := time.Now()
		latency := end.Sub(start)

		logger.Info("Request completed",
			zap.String("client_ip", c.ClientIP()),
			zap.String("method", c.Request.Method),
			zap.String("path", path),
			zap.String("query", query),
			zap.Int("status", c.Writer.Status()),
			zap.String("user_agent", c.Request.UserAgent()),
			zap.Duration("latency", latency),
			zap.Int("body_size", c.Writer.Size()),
		)
	}
}

// RateLimitMiddleware 限流中间件
func RateLimitMiddleware(config interface{}) gin.HandlerFunc {
	var rps, burst int

	if cfg, ok := config.(map[string]interface{}); ok {
		if enabled, ok := cfg["enabled"].(bool); ok && enabled {
			if rpsVal, ok := cfg["rps"].(int); ok {
				rps = rpsVal
			}
			if burstVal, ok := cfg["burst"].(int); ok {
				burst = burstVal
			}
		}
	}

	if rps == 0 {
		rps = 100
	}
	if burst == 0 {
		burst = 50
	}

	limiter := rate.NewLimiter(rate.Limit(rps), burst)

	return func(c *gin.Context) {
		if !limiter.Allow() {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error": "Too many requests",
			})
			c.Abort()
			return
		}
		c.Next()
	}
}

// CorsMiddleware CORS中间件
func CorsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS, PATCH")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Origin, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization")
		c.Writer.Header().Set("Access-Control-Expose-Headers", "Content-Length")
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}
