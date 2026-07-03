package api

import "net/http"

// handleOpenAPI serves a focused OpenAPI 3 description of the integration-facing
// API, so the platform is documented and buildable-on (point Swagger UI / an
// OpenAPI client generator at /api/openapi.json). Authenticate with an API
// token: Authorization: Bearer apt_…
func (s *Server) handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write([]byte(openAPISpec))
}

const openAPISpec = `{
  "openapi": "3.0.3",
  "info": {
    "title": "Aperture NVR API",
    "version": "1",
    "description": "Integration API for the Aperture NVR. Authenticate with a scoped API token (Settings → Integrations) as a Bearer credential; a token inherits its owner's role and camera access."
  },
  "servers": [{ "url": "/" }],
  "security": [{ "bearerAuth": [] }],
  "components": {
    "securitySchemes": {
      "bearerAuth": { "type": "http", "scheme": "bearer", "description": "An Aperture API token, e.g. apt_abcd…" }
    },
    "schemas": {
      "Camera": {
        "type": "object",
        "properties": {
          "id": { "type": "string" }, "name": { "type": "string" },
          "enabled": { "type": "boolean" }, "recordMode": { "type": "string" }
        }
      },
      "Event": {
        "type": "object",
        "properties": {
          "id": { "type": "string" }, "cameraId": { "type": "string" },
          "type": { "type": "string" }, "startTs": { "type": "integer" },
          "endTs": { "type": "integer" }, "score": { "type": "number" },
          "topLabel": { "type": "string" },
          "objects": { "type": "array", "items": { "$ref": "#/components/schemas/DetectedObject" } }
        }
      },
      "DetectedObject": {
        "type": "object",
        "properties": {
          "label": { "type": "string" }, "confidence": { "type": "number" },
          "x": { "type": "number" }, "y": { "type": "number" },
          "w": { "type": "number" }, "h": { "type": "number" }
        }
      },
      "Webhook": {
        "type": "object",
        "properties": {
          "id": { "type": "string" }, "name": { "type": "string" },
          "url": { "type": "string" }, "enabled": { "type": "boolean" },
          "events": { "type": "array", "items": { "type": "string" } }
        }
      }
    }
  },
  "paths": {
    "/api/cameras": {
      "get": {
        "summary": "List cameras the caller may see",
        "responses": { "200": { "description": "OK", "content": { "application/json": {
          "schema": { "type": "array", "items": { "$ref": "#/components/schemas/Camera" } } } } } }
      }
    },
    "/api/events": {
      "get": {
        "summary": "Recent events across cameras",
        "parameters": [
          { "name": "from", "in": "query", "schema": { "type": "integer" }, "description": "unix ms" },
          { "name": "to", "in": "query", "schema": { "type": "integer" }, "description": "unix ms" },
          { "name": "limit", "in": "query", "schema": { "type": "integer" } }
        ],
        "responses": { "200": { "description": "OK", "content": { "application/json": {
          "schema": { "type": "array", "items": { "$ref": "#/components/schemas/Event" } } } } } }
      }
    },
    "/api/cameras/{id}/events": {
      "get": {
        "summary": "Events for one camera in a time range",
        "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }],
        "responses": { "200": { "description": "OK" } }
      }
    },
    "/api/cameras/{id}/snapshot.jpg": {
      "get": {
        "summary": "Current JPEG snapshot",
        "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }],
        "responses": { "200": { "description": "JPEG", "content": { "image/jpeg": {} } } }
      }
    },
    "/api/system/health": {
      "get": { "summary": "Liveness + component health", "security": [], "responses": { "200": { "description": "OK" } } }
    },
    "/api/webhooks": {
      "get": { "summary": "List outbound webhooks (admin)", "responses": { "200": { "description": "OK" } } },
      "post": { "summary": "Create a webhook (admin)", "responses": { "200": { "description": "Created" } } }
    }
  }
}`
