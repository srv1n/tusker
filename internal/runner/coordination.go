package runner

// CoordinationCapabilities are deliberately per installed endpoint. A blank
// or false field is unsupported until conformance proves it; callers must not
// infer active delivery from ordinary session resume.
type CoordinationCapabilities struct {
	Endpoint          string `json:"endpoint"`
	Version           string `json:"version"`
	Evidence          string `json:"evidence"`
	AttachExternal    bool   `json:"attachExternal"`
	ResumeIdle        bool   `json:"resumeIdle"`
	DeliverActive     bool   `json:"deliverActive"`
	ObserveTurn       bool   `json:"observeTurn"`
	CaptureReply      bool   `json:"captureReply"`
	ReconcileDelivery bool   `json:"reconcileDelivery"`
}

func (c CoordinationCapabilities) Can(operation string) bool {
	switch operation {
	case "attach":
		return c.AttachExternal
	case "resume":
		return c.ResumeIdle
	case "active-delivery":
		return c.DeliverActive
	case "observe":
		return c.ObserveTurn
	case "reply":
		return c.CaptureReply
	case "reconcile":
		return c.ReconcileDelivery
	}
	return false
}
