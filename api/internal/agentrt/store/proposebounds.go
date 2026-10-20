package store

// The bounds of a proposed mission's words (Owner 2026-10-15 「决策卡 A」): a
// mission outside the standing envelope goes on the Owner's card whole, and
// the question store takes a card body of 3000 encoded bytes; a proposal
// within these bounds fits a card. The details go in a document the goal
// names.
const (
	MaxMissionTitleBytes   = 200
	MaxMissionGoalBytes    = 1200
	MaxMissionSuccess      = 10
	MaxMissionSuccessBytes = 200
	MaxMissionRepos        = 12
	MaxNoMergeBytes        = 200
	MaxNeedsOwnerBytes     = 200
)

// tooLong is the advice a proposal that does not fit is given.
const tooLong = "too long: put the details in a document (an artifact, or a file in a repo) and name it in the goal"

// bounded checks the words of a proposal against the bounds, whatever
// envelope is in force, so whether a proposal is taken never depends on the
// rules of the day. Nothing is cut.
func (p MissionProposal) bounded() error {
	switch {
	case len(p.Title) > MaxMissionTitleBytes:
		return invalid("the title is %d bytes, more than %d: %s", len(p.Title), MaxMissionTitleBytes, tooLong)
	case len(p.Goal) > MaxMissionGoalBytes:
		return invalid("the goal is %d bytes, more than %d: %s", len(p.Goal), MaxMissionGoalBytes, tooLong)
	case len(p.Success) > MaxMissionSuccess:
		return invalid("%d success criteria, more than %d: %s", len(p.Success), MaxMissionSuccess, tooLong)
	case len(p.Repos) > MaxMissionRepos:
		return invalid("%d repos, more than %d: propose a mission for fewer", len(p.Repos), MaxMissionRepos)
	case len(p.NoMerge) > MaxNoMergeBytes:
		return invalid("no_merge is %d bytes, more than %d: say in a sentence why the results stay off main", len(p.NoMerge), MaxNoMergeBytes)
	case len(p.NeedsOwner) > MaxNeedsOwnerBytes:
		return invalid("needs_owner is %d bytes, more than %d: say in a sentence why its cards wait for the Owner", len(p.NeedsOwner), MaxNeedsOwnerBytes)
	}
	for i, c := range p.Success {
		if len(c) > MaxMissionSuccessBytes {
			return invalid("success criterion %d is %d bytes, more than %d: %s", i+1, len(c), MaxMissionSuccessBytes, tooLong)
		}
	}
	return nil
}
