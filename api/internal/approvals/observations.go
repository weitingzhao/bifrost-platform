package approvals

// Observations returns all retained approval states without sweeping or
// holding the approval store's lock across the caller's work. Consumers keep
// durable observations of their own; absence from this bounded history is
// never a rejection, expiry or revocation.
func (s *Service) Observations() ([]Approval, error) {
	doc, err := s.store.read()
	if err != nil {
		return nil, err
	}
	out := make([]Approval, 0, len(doc.Approvals))
	for _, a := range doc.Approvals {
		out = append(out, a.public())
	}
	return out, nil
}
