package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/tesserix/dwellm8/services/api/internal/identity/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/pii"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// Staff is the firm's own team: who it employs, what each may do, which
// buildings each is responsible for, and when they work (#353).
type Staff struct {
	principals *store.Principals
	log        *slog.Logger
}

// NewStaff builds the service.
func NewStaff(p *store.Principals, log *slog.Logger) *Staff {
	return &Staff{principals: p, log: log}
}

// Re-exported shapes, so a handler need not reach into the store.
type (
	StaffRole       = store.StaffRole
	StaffMember     = store.StaffMember
	StaffAssignment = store.StaffAssignment
	StaffShift      = store.StaffShift
)

// Re-exported refusals, on the same terms.
var (
	ErrOverCap      = store.ErrOverCap
	ErrPropertyHeld = store.ErrPropertyHeld
	ErrNoStaff      = store.ErrNoStaff
)

// ErrInvalid is a request the domain refuses before the database sees it.
var ErrInvalid = errors.New("identity: that is not a team the firm can have")

// The cap band is the schema's. The default of six sits in the middle of the
// five-to-eight a firm can actually maintain.
const (
	minPropertyLimit = 1
	maxPropertyLimit = 50
)

var employmentTypes = map[string]bool{
	"full_time": true, "part_time": true, "contract": true, "intern": true,
}

var payFrequencies = map[string]bool{
	"monthly": true, "fortnightly": true, "weekly": true, "daily": true,
}

var staffStates = map[string]bool{
	"invited": true, "active": true, "suspended": true, "exited": true,
}

var panPattern = regexp.MustCompile(`^[A-Z]{5}[0-9]{4}[A-Z]$`)
var clockPattern = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)

// Team is the whole page: the roles the firm defined, the people holding them
// and who is responsible for what today.
type Team struct {
	Roles       []StaffRole
	Members     []StaffMember
	Assignments []StaffAssignment
}

// Team reads it in one call, because the app draws it as one screen.
func (s *Staff) Team(ctx context.Context, org tenancy.ID) (Team, error) {
	roles, err := s.principals.StaffRoles(ctx, org)
	if err != nil {
		return Team{}, err
	}
	members, err := s.principals.StaffMembers(ctx, org)
	if err != nil {
		return Team{}, err
	}
	assignments, err := s.principals.StaffAssignments(ctx, org)
	if err != nil {
		return Team{}, err
	}
	return Team{Roles: roles, Members: members, Assignments: assignments}, nil
}

// SaveRole names a job and says how much of one it carries.
func (s *Staff) SaveRole(ctx context.Context, org tenancy.ID, r StaffRole) (StaffRole, error) {
	r.Name = strings.TrimSpace(r.Name)
	if r.Name == "" {
		return StaffRole{}, fmt.Errorf("%w: a role needs a name", ErrInvalid)
	}
	if r.PropertyLimit < minPropertyLimit || r.PropertyLimit > maxPropertyLimit {
		return StaffRole{}, fmt.Errorf("%w: a role carries between %d and %d properties",
			ErrInvalid, minPropertyLimit, maxPropertyLimit)
	}
	if len(r.Permissions) == 0 {
		return StaffRole{}, fmt.Errorf("%w: a role that may do nothing is not a role", ErrInvalid)
	}
	for _, p := range r.Permissions {
		if !store.KnownPermission(p) {
			return StaffRole{}, fmt.Errorf("%w: %q is not something this platform checks", ErrInvalid, p)
		}
	}
	id, err := s.principals.SaveStaffRole(ctx, org, r)
	if err != nil {
		return StaffRole{}, err
	}
	r.ID = id
	return r, nil
}

// Employ puts somebody on the firm's payroll. PAN arrives whole, because the
// firm types it once; it is masked here and never held otherwise (ADR-0013).
func (s *Staff) Employ(ctx context.Context, org tenancy.ID, m StaffMember, pan string) (StaffMember, error) {
	m.FullName = strings.TrimSpace(m.FullName)
	m.Phone, m.Email = strings.TrimSpace(m.Phone), strings.TrimSpace(m.Email)
	if m.FullName == "" {
		return StaffMember{}, fmt.Errorf("%w: a colleague needs a name", ErrInvalid)
	}
	if m.Phone == "" && m.Email == "" {
		return StaffMember{}, fmt.Errorf("%w: a colleague needs a phone or an email to be reached on", ErrInvalid)
	}
	if m.SalaryMinor < 0 {
		return StaffMember{}, fmt.Errorf("%w: a salary cannot be less than nothing", ErrInvalid)
	}
	if m.EmploymentType != "" && !employmentTypes[m.EmploymentType] {
		return StaffMember{}, fmt.Errorf("%w: employment is full time, part time, contract or an internship", ErrInvalid)
	}
	if m.PayFrequency != "" && !payFrequencies[m.PayFrequency] {
		return StaffMember{}, fmt.Errorf("%w: pay is monthly, fortnightly, weekly or daily", ErrInvalid)
	}
	if m.State != "" && !staffStates[m.State] {
		return StaffMember{}, fmt.Errorf("%w: somebody is invited, active, suspended or gone", ErrInvalid)
	}
	if m.PropertyLimit != 0 && (m.PropertyLimit < minPropertyLimit || m.PropertyLimit > maxPropertyLimit) {
		return StaffMember{}, fmt.Errorf("%w: one person carries between %d and %d properties",
			ErrInvalid, minPropertyLimit, maxPropertyLimit)
	}
	if pan = strings.ToUpper(strings.TrimSpace(pan)); pan != "" {
		if !panPattern.MatchString(pan) {
			return StaffMember{}, fmt.Errorf("%w: that is not a PAN", ErrInvalid)
		}
		masked, err := pii.Mask(pii.KindPAN, pii.NewSecret(pan))
		if err != nil {
			return StaffMember{}, fmt.Errorf("identity: masking a colleague's PAN: %w", err)
		}
		m.PANMasked = masked
	}
	return s.principals.AddStaffMember(ctx, org, m)
}

// Change is what a firm may alter about somebody after they are employed.
type Change struct {
	State         string
	ExitedOn      string
	PropertyLimit int
}

// Update applies it. Somebody who leaves hands back what they held: a building
// nobody is responsible for is how a complaint goes unanswered for a month.
func (s *Staff) Update(ctx context.Context, org tenancy.ID, staffID string, c Change) error {
	if c.State == "" && c.PropertyLimit == 0 {
		return fmt.Errorf("%w: nothing to change", ErrInvalid)
	}
	if c.State != "" && !staffStates[c.State] {
		return fmt.Errorf("%w: somebody is invited, active, suspended or gone", ErrInvalid)
	}
	if c.PropertyLimit != 0 && (c.PropertyLimit < minPropertyLimit || c.PropertyLimit > maxPropertyLimit) {
		return fmt.Errorf("%w: one person carries between %d and %d properties",
			ErrInvalid, minPropertyLimit, maxPropertyLimit)
	}
	if c.PropertyLimit != 0 {
		if err := s.principals.SetStaffLimit(ctx, org, staffID, c.PropertyLimit); err != nil {
			return err
		}
	}
	if c.State == "" {
		return nil
	}
	if err := s.principals.SetStaffState(ctx, org, staffID, c.State, c.ExitedOn); err != nil {
		return err
	}
	if c.State != "exited" {
		return nil
	}
	held, err := s.principals.StaffAssignments(ctx, org)
	if err != nil {
		return err
	}
	for _, a := range held {
		if a.StaffID != staffID {
			continue
		}
		if err := s.principals.ReleaseAssignment(ctx, org, a.ID); err != nil {
			return err
		}
	}
	return nil
}

// Assign makes one manager responsible for one property.
func (s *Staff) Assign(ctx context.Context, org tenancy.ID, staffID, propertyID string) (StaffAssignment, error) {
	if staffID == "" || propertyID == "" {
		return StaffAssignment{}, fmt.Errorf("%w: an assignment names a colleague and a property", ErrInvalid)
	}
	id, err := s.principals.AssignProperty(ctx, org, staffID, propertyID)
	if err != nil {
		return StaffAssignment{}, err
	}
	return StaffAssignment{ID: id, StaffID: staffID, PropertyID: propertyID}, nil
}

// Release hands a property back today.
func (s *Staff) Release(ctx context.Context, org tenancy.ID, assignmentID string) error {
	return s.principals.ReleaseAssignment(ctx, org, assignmentID)
}

// SetRota replaces somebody's whole week.
func (s *Staff) SetRota(ctx context.Context, org tenancy.ID, staffID string, week []StaffShift) error {
	for _, sh := range week {
		if sh.Weekday < 1 || sh.Weekday > 7 {
			return fmt.Errorf("%w: a week runs Monday to Sunday, 1 to 7", ErrInvalid)
		}
		if !clockPattern.MatchString(sh.StartsAt) || !clockPattern.MatchString(sh.EndsAt) {
			return fmt.Errorf("%w: a shift starts and ends at a time of day, as HH:MM", ErrInvalid)
		}
		if sh.EndsAt <= sh.StartsAt {
			return fmt.Errorf("%w: a shift ends after it starts — overnight cover is two shifts", ErrInvalid)
		}
	}
	return s.principals.SetStaffShifts(ctx, org, staffID, week)
}

// Rota is one person's week.
func (s *Staff) Rota(ctx context.Context, org tenancy.ID, staffID string) ([]StaffShift, error) {
	return s.principals.StaffShifts(ctx, org, staffID)
}
