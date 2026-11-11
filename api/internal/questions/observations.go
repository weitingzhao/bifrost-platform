package questions

// Observations returns every retained question without sweeping or writing.
// Absence from this bounded history does not imply a state transition.
func (s *Service) Observations() ([]Question, error) {
	doc, err := s.load()
	if err != nil {
		return nil, err
	}
	out := make([]Question, 0, len(doc.Questions))
	for _, q := range doc.Questions {
		out = append(out, q.public(""))
	}
	return out, nil
}
