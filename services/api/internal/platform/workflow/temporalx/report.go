package temporalx

import "github.com/tesserix/dwellm8/services/api/internal/platform/workflow"

// Report is a saga Result in a form Temporal can carry.
//
// Result holds errors, and an error does not survive the payload converter: it
// marshals to an object and unmarshals to nothing. Returning one from a workflow
// loses precisely the fields somebody reads during an incident — which step
// failed, and why a compensation did not apply — and it loses them at the moment
// the workflow completes, when nobody is looking yet.
type Report struct {
	WorkflowID  string           `json:"workflow_id"`
	Outcome     workflow.Outcome `json:"outcome"`
	Completed   []string         `json:"completed,omitempty"`
	Compensated []string         `json:"compensated,omitempty"`
	FailedStep  string           `json:"failed_step,omitempty"`
	// Err is the failing step's error as text, because that is what a person
	// reads and the only form that crosses the wire intact.
	Err string `json:"error,omitempty"`
	// CompensationErrs is every compensation that could not be applied. Non-empty
	// means the outcome is escalated, always.
	CompensationErrs map[string]string       `json:"compensation_errors,omitempty"`
	PastNoReturn     bool                    `json:"past_no_return"`
	Notifications    []workflow.Notification `json:"notifications,omitempty"`
}

// reportOf converts a Result, flattening its errors to text.
func reportOf(r workflow.Result) Report {
	out := Report{
		WorkflowID:    r.WorkflowID,
		Outcome:       r.Outcome,
		Completed:     r.Completed,
		Compensated:   r.Compensated,
		FailedStep:    r.FailedStep,
		PastNoReturn:  r.PastNoReturn,
		Notifications: r.Notifications,
	}
	if r.Err != nil {
		out.Err = r.Err.Error()
	}
	if len(r.CompensationErrs) > 0 {
		out.CompensationErrs = make(map[string]string, len(r.CompensationErrs))
		for step, err := range r.CompensationErrs {
			if err != nil {
				out.CompensationErrs[step] = err.Error()
			}
		}
	}
	return out
}

// Escalated reports whether a person has to act on this run. It is the question
// an alert asks, so it is answered here rather than by everybody re-deriving it.
func (r Report) Escalated() bool {
	return r.Outcome == workflow.Escalated || len(r.CompensationErrs) > 0
}
