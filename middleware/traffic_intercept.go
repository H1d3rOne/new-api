package middleware

import (
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

// TrafficIntercept applies the request-side part of traffic interception inside
// authenticated relay groups. The upstream request/response rewrite is applied
// in relay/channel/api_request.go just before and after the provider call.
func TrafficIntercept() gin.HandlerFunc {
	return func(c *gin.Context) {
		if service.ApplyTrafficLiveInboundInterceptor(c) {
			return
		}
		if service.ApplyTrafficInboundInterceptor(c) {
			return
		}
		c.Next()
	}
}
