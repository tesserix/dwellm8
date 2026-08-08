package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// The managers a firm employs, and the properties each is responsible for
// (#353, schema chapter 356). The employment record sits beside
// organisation_members rather than inside it: a salary has no business on the
// hot path of an authorisation check.

// ErrOverCap is one manager given more properties than their role allows. The
// database refuses it; this is the same refusal in a form a handler can answer.
var ErrOverCap = errors.New("identity: that manager already holds as many properties as the role allows")

// ErrPropertyHeld is a building somebody else is already responsible for.
var ErrPropertyHeld = errors.New("identity: another manager is already responsible for that property")

// ErrNoStaff is no such person on this firm's payroll.
var ErrNoStaff = errors.New("identity: no such member of staff")

// KnownPermission answers whether the platform checks this permission at all.
// The vocabulary is chapter 010's and is closed — the same list a mandate is
// granted from, because a role that may do something no grant can confer is a
// promise nothing keeps.
func KnownPermission(p string) bool { return slices.Contains(managementPermissions, p) }

// StaffRole is what a firm calls a job, and how much of one it may carry.
type StaffRole struct {
	ID            string
	Name          string
	Permissions   []string
	PropertyLimit int
	People        int
}

// StaffMember is the employment record of somebody the firm employs.
//
// Dates are strings for the reason PartyDocument's are: they arrive from a form
// as YYYY-MM-DD and a time.Time round trip through a timezone moves a joining
// date by a day.
type StaffMember struct {
	ID       string
	PartyID  string
	RoleID   string
	RoleName string

	FullName     string
	EmployeeCode string
	Designation  string
	Email        string
	Phone        string

	EmploymentType string
	JoinedOn       string
	ExitedOn       string

	// ADR-0013: the mask, never the number. The column has a CHECK to match.
	PANMasked      string
	SalaryMinor    int64
	SalaryCurrency string
	PayFrequency   string

	EmergencyName  string
	EmergencyPhone string

	State string
	// PropertyLimit is what applies to this person: their own if the firm gave
	// them one, otherwise the role's.
	PropertyLimit int
	// Held is how many properties they are responsible for today.
	Held int
}

// StaffAssignment is one property one manager is responsible for.
type StaffAssignment struct {
	ID           string
	StaffID      string
	StaffName    string
	PropertyID   string
	PropertyName string
	ValidFrom    string
	ValidTo      string
}

// StaffShift is one working window in the week. Overnight cover is two rows.
type StaffShift struct {
	Weekday  int
	StartsAt string
	EndsAt   string
}

// SaveStaffRole creates a role or restates one the firm already named.
func (s *Principals) SaveStaffRole(ctx context.Context, org tenancy.ID, r StaffRole) (string, error) {
	var id string
	err := tenancy.Platform(ctx, s.platform, "saving a staff role",
		func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, `
				INSERT INTO staff_roles (tenant_id, name, permissions, property_limit)
				VALUES ($1::uuid, $2, $3, $4)
				ON CONFLICT (tenant_id, name) DO UPDATE
				   SET permissions = excluded.permissions,
				       property_limit = excluded.property_limit,
				       retired_at = NULL
				RETURNING id::text`,
				string(org), r.Name, r.Permissions, r.PropertyLimit).Scan(&id)
		})
	if err != nil {
		return "", fmt.Errorf("identity: saving a staff role: %w", err)
	}
	return id, nil
}

// StaffRoles is the firm's roles, with how many people hold each.
func (s *Principals) StaffRoles(ctx context.Context, org tenancy.ID) ([]StaffRole, error) {
	var out []StaffRole
	err := tenancy.Platform(ctx, s.platform, "reading staff roles",
		func(ctx context.Context, tx pgx.Tx) error {
			rows, err := tx.Query(ctx, `
				SELECT r.id::text, r.name::text, r.permissions, r.property_limit,
				       count(m.id) FILTER (WHERE m.state <> 'exited')
				  FROM staff_roles r
				  LEFT JOIN staff_members m ON m.role_id = r.id
				 WHERE r.tenant_id = $1::uuid AND r.retired_at IS NULL
				 GROUP BY r.id, r.name, r.permissions, r.property_limit
				 ORDER BY r.name`, string(org))
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var r StaffRole
				if err := rows.Scan(&r.ID, &r.Name, &r.Permissions, &r.PropertyLimit, &r.People); err != nil {
					return err
				}
				out = append(out, r)
			}
			return rows.Err()
		})
	if err != nil {
		return nil, fmt.Errorf("identity: reading staff roles: %w", err)
	}
	return out, nil
}

// AddStaffMember puts somebody on the firm's payroll. A colleague who has not
// signed in yet still gets an identity to hold the employment against, exactly
// as a reserved owner does (#240) — it is claimed at their first sign-in.
func (s *Principals) AddStaffMember(ctx context.Context, org tenancy.ID, m StaffMember) (StaffMember, error) {
	err := tenancy.Platform(ctx, s.platform, "employing a manager",
		func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, `
				INSERT INTO staff_members (
					tenant_id, party_id, role_id, full_name, employee_code, designation,
					email, phone, employment_type, joined_on, pan_masked,
					salary_minor, salary_currency, pay_frequency,
					emergency_name, emergency_phone, state, property_limit)
				VALUES ($1::uuid, coalesce(nullif($2,'')::uuid, gen_random_uuid()),
				        nullif($3,'')::uuid, $4, nullif($5,''), nullif($6,''),
				        nullif($7,''), nullif($8,''), coalesce(nullif($9,''),'full_time'),
				        coalesce(nullif($10,'')::date, current_date), nullif($11,''),
				        nullif($12,0), coalesce(nullif($13,''),'INR'), coalesce(nullif($14,''),'monthly'),
				        nullif($15,''), nullif($16,''), coalesce(nullif($17,''),'invited'),
				        nullif($18,0))
				RETURNING id::text, party_id::text`,
				string(org), m.PartyID, m.RoleID, m.FullName, m.EmployeeCode, m.Designation,
				m.Email, m.Phone, m.EmploymentType, m.JoinedOn, m.PANMasked,
				m.SalaryMinor, m.SalaryCurrency, m.PayFrequency,
				m.EmergencyName, m.EmergencyPhone, m.State, m.PropertyLimit).
				Scan(&m.ID, &m.PartyID)
		})
	if err != nil {
		return StaffMember{}, fmt.Errorf("identity: employing a manager: %w", err)
	}
	return m, nil
}

// StaffMembers is the firm's team, each with the load they carry.
func (s *Principals) StaffMembers(ctx context.Context, org tenancy.ID) ([]StaffMember, error) {
	var out []StaffMember
	err := tenancy.Platform(ctx, s.platform, "reading the team",
		func(ctx context.Context, tx pgx.Tx) error {
			rows, err := tx.Query(ctx, `
				SELECT m.id::text, m.party_id::text, coalesce(m.role_id::text,''),
				       coalesce(r.name::text,''), m.full_name,
				       coalesce(m.employee_code::text,''), coalesce(m.designation,''),
				       coalesce(m.email,''), coalesce(m.phone,''),
				       m.employment_type, to_char(m.joined_on,'YYYY-MM-DD'),
				       coalesce(to_char(m.exited_on,'YYYY-MM-DD'),''),
				       coalesce(m.pan_masked,''), coalesce(m.salary_minor,0),
				       m.salary_currency, m.pay_frequency,
				       coalesce(m.emergency_name,''), coalesce(m.emergency_phone,''),
				       m.state, coalesce(m.property_limit, r.property_limit, 6),
				       (SELECT count(*) FROM staff_assignments a
				         WHERE a.staff_id = m.id
				           AND (a.valid_to IS NULL OR a.valid_to > current_date))
				  FROM staff_members m
				  LEFT JOIN staff_roles r ON r.id = m.role_id
				 WHERE m.tenant_id = $1::uuid
				 ORDER BY m.state = 'exited', m.full_name`, string(org))
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var m StaffMember
				if err := rows.Scan(&m.ID, &m.PartyID, &m.RoleID, &m.RoleName, &m.FullName,
					&m.EmployeeCode, &m.Designation, &m.Email, &m.Phone,
					&m.EmploymentType, &m.JoinedOn, &m.ExitedOn, &m.PANMasked,
					&m.SalaryMinor, &m.SalaryCurrency, &m.PayFrequency,
					&m.EmergencyName, &m.EmergencyPhone, &m.State,
					&m.PropertyLimit, &m.Held); err != nil {
					return err
				}
				out = append(out, m)
			}
			return rows.Err()
		})
	if err != nil {
		return nil, fmt.Errorf("identity: reading the team: %w", err)
	}
	return out, nil
}

// SetStaffLimit gives one person a workload of their own, above or below what
// their role carries.
func (s *Principals) SetStaffLimit(ctx context.Context, org tenancy.ID, staffID string, limit int) error {
	return s.updateStaff(ctx, org, staffID, "setting a workload", `
		UPDATE staff_members SET property_limit = nullif($3,0)
		 WHERE tenant_id = $1::uuid AND id = $2::uuid`, limit)
}

// SetStaffState moves somebody between invited, active, suspended and exited.
// The leaving date is the table's business: exited without one is refused.
func (s *Principals) SetStaffState(ctx context.Context, org tenancy.ID, staffID, state, exitedOn string) error {
	return s.updateStaff(ctx, org, staffID, "changing an employment state", `
		UPDATE staff_members
		   SET state = $3,
		       exited_on = CASE WHEN $3 = 'exited'
		                        THEN coalesce(nullif($4,'')::date, current_date)
		                        ELSE NULL END
		 WHERE tenant_id = $1::uuid AND id = $2::uuid`, state, exitedOn)
}

func (s *Principals) updateStaff(ctx context.Context, org tenancy.ID, staffID, what, sql string, args ...any) error {
	err := tenancy.Platform(ctx, s.platform, what, func(ctx context.Context, tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, sql, append([]any{string(org), staffID}, args...)...)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNoStaff
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrNoStaff) {
			return err
		}
		return fmt.Errorf("identity: %s: %w", what, err)
	}
	return nil
}

// AssignProperty makes one manager responsible for one property. The owner of
// the property is read here rather than taken from the caller: it is what the
// row-level policy asks is_delegated about, so a firm cannot name a building it
// holds no mandate over.
func (s *Principals) AssignProperty(ctx context.Context, org tenancy.ID, staffID, propertyID string) (string, error) {
	var id string
	err := tenancy.Platform(ctx, s.platform, "assigning a property",
		func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, `
				INSERT INTO staff_assignments (tenant_id, staff_id, property_id, owner_tenant_id)
				SELECT $1::uuid, $2::uuid, p.id, p.tenant_id
				  FROM properties p
				 WHERE p.id = $3::uuid
				RETURNING id::text`, string(org), staffID, propertyID).Scan(&id)
		})
	switch {
	case err == nil:
		return id, nil
	case errors.Is(err, pgx.ErrNoRows):
		return "", fmt.Errorf("identity: assigning a property: %w", ErrNoStaff)
	case overCap(err):
		return "", ErrOverCap
	case exclusion(err, "staff_assignments_one_manager"):
		return "", ErrPropertyHeld
	default:
		return "", fmt.Errorf("identity: assigning a property: %w", err)
	}
}

// ReleaseAssignment ends one today. The row stays: who was responsible when is
// the question a complaint about last month asks.
func (s *Principals) ReleaseAssignment(ctx context.Context, org tenancy.ID, assignmentID string) error {
	err := tenancy.Platform(ctx, s.platform, "handing a property back",
		func(ctx context.Context, tx pgx.Tx) error {
			tag, err := tx.Exec(ctx, `
				UPDATE staff_assignments
				   SET valid_to = greatest(valid_from, current_date)
				 WHERE tenant_id = $1::uuid AND id = $2::uuid AND valid_to IS NULL`,
				string(org), assignmentID)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				return ErrNoStaff
			}
			return nil
		})
	if err != nil && !errors.Is(err, ErrNoStaff) {
		return fmt.Errorf("identity: handing a property back: %w", err)
	}
	return err
}

// StaffAssignments is who is responsible for what, today.
func (s *Principals) StaffAssignments(ctx context.Context, org tenancy.ID) ([]StaffAssignment, error) {
	var out []StaffAssignment
	err := tenancy.Platform(ctx, s.platform, "reading who holds what",
		func(ctx context.Context, tx pgx.Tx) error {
			rows, err := tx.Query(ctx, `
				SELECT a.id::text, a.staff_id::text, m.full_name,
				       a.property_id::text, p.name,
				       to_char(a.valid_from,'YYYY-MM-DD'),
				       coalesce(to_char(a.valid_to,'YYYY-MM-DD'),'')
				  FROM staff_assignments a
				  JOIN staff_members m ON m.id = a.staff_id
				  JOIN properties p ON p.id = a.property_id
				 WHERE a.tenant_id = $1::uuid
				   AND (a.valid_to IS NULL OR a.valid_to > current_date)
				 ORDER BY m.full_name, p.name`, string(org))
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var a StaffAssignment
				if err := rows.Scan(&a.ID, &a.StaffID, &a.StaffName, &a.PropertyID,
					&a.PropertyName, &a.ValidFrom, &a.ValidTo); err != nil {
					return err
				}
				out = append(out, a)
			}
			return rows.Err()
		})
	if err != nil {
		return nil, fmt.Errorf("identity: reading who holds what: %w", err)
	}
	return out, nil
}

// SetStaffShifts replaces the whole week. A rota is edited as a week, so a
// shift dropped from the form is dropped from the rota — retired rather than
// deleted, because nothing in this schema is deleted.
func (s *Principals) SetStaffShifts(ctx context.Context, org tenancy.ID, staffID string, week []StaffShift) error {
	err := tenancy.Platform(ctx, s.platform, "setting a rota",
		func(ctx context.Context, tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `
				UPDATE staff_shifts SET retired_at = now()
				 WHERE tenant_id = $1::uuid AND staff_id = $2::uuid AND retired_at IS NULL`,
				string(org), staffID); err != nil {
				return err
			}
			for _, sh := range week {
				if _, err := tx.Exec(ctx, `
					INSERT INTO staff_shifts (tenant_id, staff_id, weekday, starts_at, ends_at)
					VALUES ($1::uuid, $2::uuid, $3, $4::time, $5::time)`,
					string(org), staffID, sh.Weekday, sh.StartsAt, sh.EndsAt); err != nil {
					return err
				}
			}
			return nil
		})
	if err != nil {
		return fmt.Errorf("identity: setting a rota: %w", err)
	}
	return nil
}

// StaffShifts is one person's week, in the order it is worked.
func (s *Principals) StaffShifts(ctx context.Context, org tenancy.ID, staffID string) ([]StaffShift, error) {
	var out []StaffShift
	err := tenancy.Platform(ctx, s.platform, "reading a rota",
		func(ctx context.Context, tx pgx.Tx) error {
			rows, err := tx.Query(ctx, `
				SELECT weekday, to_char(starts_at,'HH24:MI'), to_char(ends_at,'HH24:MI')
				  FROM staff_shifts
				 WHERE tenant_id = $1::uuid AND staff_id = $2::uuid
				   AND retired_at IS NULL
				 ORDER BY weekday, starts_at`, string(org), staffID)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var sh StaffShift
				if err := rows.Scan(&sh.Weekday, &sh.StartsAt, &sh.EndsAt); err != nil {
					return err
				}
				out = append(out, sh)
			}
			return rows.Err()
		})
	if err != nil {
		return nil, fmt.Errorf("identity: reading a rota: %w", err)
	}
	return out, nil
}

// The cap is a constraint trigger, so it arrives as a check violation naming
// itself rather than as a constraint name pgx can match on.
func overCap(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && strings.Contains(pg.Message, "staff_assignments_over_cap")
}

func exclusion(err error, constraint string) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23P01" && pg.ConstraintName == constraint
}
