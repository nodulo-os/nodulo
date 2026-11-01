package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// InstitutionalSettings contains the optional public identity configured by
// each installation. Empty fields are intentionally valid.
type InstitutionalSettings struct {
	ShowInstitution    bool      `json:"show_institution"`
	InstitutionName    string    `json:"institution_name"`
	InstitutionAcronym string    `json:"institution_acronym"`
	InstitutionUnit    string    `json:"institution_unit"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type InstitutionalSettingsRepository struct {
	pool *pgxpool.Pool
}

func NewInstitutionalSettingsRepository(pool *pgxpool.Pool) *InstitutionalSettingsRepository {
	return &InstitutionalSettingsRepository{pool: pool}
}

func (r *InstitutionalSettingsRepository) Get(ctx context.Context) (InstitutionalSettings, error) {
	var settings InstitutionalSettings
	err := r.pool.QueryRow(ctx, `SELECT show_institution, institution_name, institution_acronym, institution_unit, updated_at
		FROM institutional_settings WHERE id = TRUE`).Scan(
		&settings.ShowInstitution, &settings.InstitutionName, &settings.InstitutionAcronym,
		&settings.InstitutionUnit, &settings.UpdatedAt,
	)
	if err != nil {
		return InstitutionalSettings{}, fmt.Errorf("get institutional settings: %w", err)
	}
	return settings, nil
}

func (r *InstitutionalSettingsRepository) Update(ctx context.Context, settings InstitutionalSettings, actorID string) (InstitutionalSettings, error) {
	_, err := r.pool.Exec(ctx, `INSERT INTO institutional_settings
		(id, show_institution, institution_name, institution_acronym, institution_unit, updated_by, updated_at)
		VALUES (TRUE, $1, $2, $3, $4, $5, CURRENT_TIMESTAMP)
		ON CONFLICT (id) DO UPDATE SET
		show_institution = EXCLUDED.show_institution,
		institution_name = EXCLUDED.institution_name,
		institution_acronym = EXCLUDED.institution_acronym,
		institution_unit = EXCLUDED.institution_unit,
		updated_by = EXCLUDED.updated_by,
		updated_at = EXCLUDED.updated_at`, settings.ShowInstitution, settings.InstitutionName,
		settings.InstitutionAcronym, settings.InstitutionUnit, actorID)
	if err != nil {
		return InstitutionalSettings{}, fmt.Errorf("update institutional settings: %w", err)
	}
	return r.Get(ctx)
}
