package middleware

import (
	"bytes"
	"io"
	"time"

	"github.com/gin-contrib/requestid"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
)

func NewRequestResponseLogger(skipLogPaths map[string]struct{}) gin.HandlerFunc {
	return func(c *gin.Context) {
		if _, contained := skipLogPaths[c.Request.URL.Path]; contained {
			return
		}

		startTime := time.Now()
		logger := log.With().Str("req.id", requestid.Get(c)).Logger()

		// Attach the logger to the request context.
		// So as long as the request's context is add to the log calls,
		// the request ID will be included in the logs.
		ctx := logger.WithContext(c.Request.Context())
		c.Request = c.Request.WithContext(ctx)

		var buf bytes.Buffer
		tee := io.TeeReader(c.Request.Body, &buf)
		reqBody, _ := io.ReadAll(tee)
		c.Request.Body = io.NopCloser(&buf)

		blw := &bodyLogWriter{body: bytes.NewBufferString(""), ResponseWriter: c.Writer}
		c.Writer = blw

		c.Next()

		resp, _ := io.ReadAll(blw.body)

		status := c.Writer.Status()
		logger.
			Info().
			Str("req.method", c.Request.Method).
			Bytes("req.body", reqBody).
			Str("req.uri", c.Request.URL.RequestURI()).
			Str("req.ip", c.ClientIP()).
			Str("req.ua", c.Request.UserAgent()).
			Int("resp.status", status).
			Bytes("resp.body", resp).
			Dur("resp.ms", time.Since(startTime)).
			Msg("handled request")
	}
}

type bodyLogWriter struct {
	gin.ResponseWriter
	body *bytes.Buffer
}

func (w bodyLogWriter) Write(b []byte) (int, error) {
	w.body.Write(b)
	return w.ResponseWriter.Write(b)
}

func (w bodyLogWriter) WriteString(s string) (int, error) {
	w.body.WriteString(s)
	return w.ResponseWriter.WriteString(s)
}
