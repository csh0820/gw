package health

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"time"
)

func Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":    200,
		"message":   http.StatusText(200),
		"timestamp": time.Now().Unix(),
	})
}
