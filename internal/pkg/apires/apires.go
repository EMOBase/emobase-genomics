package apires

import (
	"net/http"

	"github.com/gin-contrib/requestid"
	"github.com/gin-gonic/gin"
)

func OK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, gin.H{"data": data, "requestId": requestid.Get(c)})
}

func Created(c *gin.Context, data any) {
	c.JSON(http.StatusCreated, gin.H{"data": data, "requestId": requestid.Get(c)})
}

func Fail(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{"error": message, "requestId": requestid.Get(c)})
}

func AbortFail(c *gin.Context, status int, message string) {
	c.AbortWithStatusJSON(status, gin.H{"error": message, "requestId": requestid.Get(c)})
}
