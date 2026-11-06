-- agentrt 0012: the landing CI as an implementation's check (R5:5.7-9,
-- R5:5.7-10; the Owner 2026-11-05). Where the landing runs a CI, a
-- submitted implementation is accepted on submission and the landing CI,
-- which tests the commit replayed on main, is its check: the acceptance is
-- a verification of method landing_ci, and a red landing CI is recorded as
-- a failed landing_ci verification of the attempt it tested. That sends the
-- task back for another round, whose attempt is accepted in turn: a task may
-- now have more than one accepting verification, one per attempt, and the
-- latest is the accepted attempt (every reader already orders them so).
--
-- Upgrading: run this before the platform that writes landing_ci. An older
-- platform still reads the rows this one writes: the latest accepting
-- verification names the accepted attempt either way.
ALTER TABLE agentrt.verification DROP CONSTRAINT verification_method_check;
ALTER TABLE agentrt.verification ADD CONSTRAINT verification_method_check
  CHECK (method IN ('deterministic','review','gate','landing_ci'));
-- A landing CI verification is the controller's: it has no verifier attempt.
ALTER TABLE agentrt.verification ADD CONSTRAINT verification_landing_ci_check
  CHECK (method <> 'landing_ci' OR verifier_attempt_id IS NULL);
DROP INDEX agentrt.verification_one_accept;
CREATE UNIQUE INDEX verification_one_accept ON agentrt.verification (attempt_id) WHERE accepts_attempt;
