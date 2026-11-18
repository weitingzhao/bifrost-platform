-- agentrt 0031: the daily full CI's record is its own event type (foundation
-- line, 2026-11-17; one owning domain per event type, step 3a of the note and
-- policy_alert split). The controller's notes of the runs a day started and of
-- each red run were note events with a full_ci field. They become
-- full_ci_noted {text, full_ci}, declared by the verification domain's
-- evidence package and registered under verification. The old rows stay and
-- are still read (evidence reads both).
--
-- The one statement widens event_type_closed: 0030's list and the new type.
-- Nothing is read for change, changed or deleted. Upgrading: run this before
-- the platform that writes the new type; an older platform still writes the
-- old note. The ALTER's lock and statement are bounded by migrate.apply (2s,
-- 30s), and a timeout rolls the file back.
-- Way back: 0030's constraint, once no full_ci_noted event is left.

ALTER TABLE agentrt.event DROP CONSTRAINT event_type_closed, ADD CONSTRAINT event_type_closed CHECK (event_type IN (
 'plan_node_created','plan_node_retitled','plan_node_state_changed','plan_change_proposed',
 'plan_change_decided','plan_changed','mission_proposed','mission_state_changed','mission_amended',
 'task_proposed','task_state_changed','task_dependency_added','task_cancelled','route_decided',
 'attempt_leased','attempt_started','attempt_progress','attempt_checkpointed','attempt_stale',
 'attempt_renewed','attempt_computed','attempt_persisted','attempt_submitted','attempt_lost',
 'attempt_ended','attempt_salvaged','attempt_cancelled','artifact_put','verification_recorded',
 'owner_wait_opened','owner_wait_closed','approval_seen','question_seen','ci_seen','pipeline_seen',
 'deploy_seen','owner_signoff','actor_registered','host_registered','host_state_changed','host_retired',
 'control_set','control_cleared','rule_proposed','rule_signed','rule_tightened','rule_expired',
 'rule_approved','capability_scored','candidate_status_changed','budget_charged','pool_cooldown_set',
 'pool_measured','message_posted','message_acked','policy_alert','decision_recorded','host_slots_set',
 'wave_opened','wave_closed','wave_abandoned','note','wait_limit_passed','legacy_imported',
 'metrics_computed','metric_label_set','ci_reused','ci_red_classified','git.main.advanced',
 'git.ready.pushed','git.tag.changed','git.mirror.settled','git.pr.changed','ci.run.started',
 'ci.run.finished','ci.verdict.changed','release.deploy.finished','release.gitops.changed',
 'release.window.changed','release.freeze.changed','release.policy.expiring','approval.card.created',
 'approval.card.decided','approval.card.executed','owner.question.asked','owner.question.answered',
 'seat.mail.arrived','seat.status.changed','seat.liveness.changed','seat.work.changed',
 'seat.session.changed','alert.rule.fired','alert.channel.changed','loop.health.changed',
 'env.health.changed','plugin.status.changed','cluster.workload.changed','schema.migration.applied',
 'data.job.finished','audit.action.recorded','plan.fact.changed','codehealth.report.published',
 'dev.session.changed','reconcile.drift.found',
 'controller_loop_failing','controller_loop_recovered',
 'artifact_refused',
 'full_ci_noted'));
