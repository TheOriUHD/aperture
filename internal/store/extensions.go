package store

import "time"

// Extension is the registry row for an installed extension (Extension System,
// T1.1). Its files live on disk under Config.ExtensionsDir()/<id>/; this row is
// the metadata — backed up by ExportConfig, reconciled against disk after a
// restore. Scopes/CameraGrants are the API authority the extension's token (and
// the UI broker's synthesized claim) are minted with — never exceeding the
// installer. ManifestJSON and ExtUID are never serialized to clients.
type Extension struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Version      string   `json:"version"`
	Source       string   `json:"source"` // store | sideload
	UpdateURL    string   `json:"updateUrl"`
	ManifestJSON string   `json:"-"`
	Scopes       []string `json:"scopes"`
	CameraGrants []string `json:"cameraGrants"`
	Enabled      bool     `json:"enabled"`
	ExtUID       int      `json:"-"`
	InstalledBy  string   `json:"installedBy"`
	InstalledAt  int64    `json:"installedAt"`
	UpdatedAt    int64    `json:"updatedAt"`
	Pinned       bool     `json:"pinned"`   // user granted a nav-rail entry (T2.1)
	Modified     bool     `json:"modified"` // edited in the in-app editor (T2.3)
}

const extensionCols = `id, name, version, source, update_url, manifest_json, scopes, camera_grants, enabled, ext_uid, installed_by, installed_at, updated_at, pinned, modified`

func scanExtension(row rowScanner) (*Extension, error) {
	var e Extension
	var scopes, grants string
	var enabled, pinned, modified int
	if err := row.Scan(&e.ID, &e.Name, &e.Version, &e.Source, &e.UpdateURL, &e.ManifestJSON,
		&scopes, &grants, &enabled, &e.ExtUID, &e.InstalledBy, &e.InstalledAt, &e.UpdatedAt, &pinned, &modified); err != nil {
		return nil, err
	}
	e.Scopes, e.CameraGrants = csvSplit(scopes), csvSplit(grants)
	e.Enabled, e.Pinned, e.Modified = enabled != 0, pinned != 0, modified != 0
	return &e, nil
}

// CreateExtension inserts a freshly-installed extension. InstalledAt defaults to
// now; UpdatedAt is always stamped now.
func (s *Store) CreateExtension(e *Extension) error {
	now := time.Now().UnixMilli()
	if e.InstalledAt == 0 {
		e.InstalledAt = now
	}
	e.UpdatedAt = now
	_, err := s.db.Exec(
		`INSERT INTO extensions (`+extensionCols+`)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.Name, e.Version, e.Source, e.UpdateURL, e.ManifestJSON,
		csvJoin(e.Scopes), csvJoin(e.CameraGrants), boolInt(e.Enabled), e.ExtUID, e.InstalledBy, e.InstalledAt, e.UpdatedAt,
		boolInt(e.Pinned), boolInt(e.Modified))
	return err
}

func (s *Store) GetExtension(id string) (*Extension, error) {
	return scanExtension(s.db.QueryRow(`SELECT `+extensionCols+` FROM extensions WHERE id = ?`, id))
}

func (s *Store) ListExtensions() ([]*Extension, error) {
	rows, err := s.db.Query(`SELECT ` + extensionCols + ` FROM extensions ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Extension
	for rows.Next() {
		e, err := scanExtension(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// SetExtensionEnabled flips the enabled flag (the manager's enable/disable).
func (s *Store) SetExtensionEnabled(id string, enabled bool) error {
	return affected(s.db.Exec(`UPDATE extensions SET enabled = ?, updated_at = ? WHERE id = ?`,
		boolInt(enabled), time.Now().UnixMilli(), id))
}

// SetExtensionPinned flips the user's nav-rail grant (T2.1). Pinning is a UI
// decision, independent of what the manifest declares.
func (s *Store) SetExtensionPinned(id string, pinned bool) error {
	return affected(s.db.Exec(`UPDATE extensions SET pinned = ?, updated_at = ? WHERE id = ?`,
		boolInt(pinned), time.Now().UnixMilli(), id))
}

// SetExtensionModified records whether the on-disk files diverge from the installed
// bundle (set true when edited in-app, cleared on a clean update/reinstall) (T2.3).
func (s *Store) SetExtensionModified(id string, modified bool) error {
	return affected(s.db.Exec(`UPDATE extensions SET modified = ?, updated_at = ? WHERE id = ?`,
		boolInt(modified), time.Now().UnixMilli(), id))
}

// UpdateExtension replaces the mutable fields on a re-install or update, keeping
// the identity, uid, and install provenance (id, ext_uid, installed_at,
// installed_by, enabled are untouched).
func (s *Store) UpdateExtension(e *Extension) error {
	return affected(s.db.Exec(
		`UPDATE extensions SET name = ?, version = ?, source = ?, update_url = ?, manifest_json = ?, scopes = ?, camera_grants = ?, updated_at = ? WHERE id = ?`,
		e.Name, e.Version, e.Source, e.UpdateURL, e.ManifestJSON, csvJoin(e.Scopes), csvJoin(e.CameraGrants), time.Now().UnixMilli(), e.ID))
}

// DeleteExtension removes the row (uninstall — the caller also removes the
// on-disk files, revokes the token, and stops the process).
func (s *Store) DeleteExtension(id string) error {
	return affected(s.db.Exec(`DELETE FROM extensions WHERE id = ?`, id))
}
