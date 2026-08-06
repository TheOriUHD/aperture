package api

import "strings"

// Scopes an API token can hold. Session and CLI credentials implicitly hold all
// scopes; a token holds only those granted and can never exceed its owner's role
// or camera ACL. The dangerous admin operations (users, tokens, backup, restore,
// restart, purge, reindex) are deliberately NOT scopes — they are session-only
// (see the sessionOnly specs), so no token can reach them regardless of scope.
//
// Routes begin gating on specific scopes in Phase 3 (the resource model). Until
// then the taxonomy is defined and validated at token creation, but the enforce
// scope check is dormant because no route declares a required scope.
const (
	ScopeCamerasRead    = "cameras:read"
	ScopeCamerasWrite   = "cameras:write"
	ScopeEventsRead     = "events:read"
	ScopeEventsWrite    = "events:write"
	ScopeCoverageRead   = "coverage:read"
	ScopeMediaRead      = "media:read"
	ScopeMediaWrite     = "media:write"
	ScopeMediaLive      = "media:live"
	ScopeStorageRead    = "storage:read"
	ScopeStorageWrite   = "storage:write"
	ScopeSchedulesRead  = "schedules:read"
	ScopeSchedulesWrite = "schedules:write"
	ScopeZonesRead      = "zones:read"
	ScopeZonesWrite     = "zones:write"
	ScopeDetectionRead  = "detection:read"
	ScopeDetectionWrite = "detection:write"
	ScopeSiteRead       = "site:read"
	ScopeSiteWrite      = "site:write"
	ScopeAutomationRead  = "automation:read"
	ScopeAutomationWrite = "automation:write"
	ScopeAlarmArm        = "alarm:arm"
	ScopeSystemRead      = "system:read"
)

// knownScopes is the set of assignable scopes; token creation rejects anything
// outside it so a typo can't silently mint an over- or under-powered token.
var knownScopes = map[string]bool{
	ScopeCamerasRead: true, ScopeCamerasWrite: true,
	ScopeEventsRead: true, ScopeEventsWrite: true,
	ScopeCoverageRead: true,
	ScopeMediaRead:   true, ScopeMediaWrite: true, ScopeMediaLive: true,
	ScopeStorageRead: true, ScopeStorageWrite: true,
	ScopeSchedulesRead: true, ScopeSchedulesWrite: true,
	ScopeZonesRead:     true, ScopeZonesWrite: true,
	ScopeDetectionRead: true, ScopeDetectionWrite: true,
	ScopeSiteRead:       true, ScopeSiteWrite: true,
	ScopeAutomationRead: true, ScopeAutomationWrite: true,
	ScopeAlarmArm:   true,
	ScopeSystemRead: true,
}

// validateScopes returns a comma-joined list of any requested scopes that are
// not assignable, or "" if every scope is known.
func validateScopes(scopes []string) string {
	var bad []string
	for _, s := range scopes {
		if !knownScopes[s] {
			bad = append(bad, s)
		}
	}
	return strings.Join(bad, ", ")
}
