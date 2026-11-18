-- agentrt 0032: eight more records become their own event types (foundation
-- line, 2026-11-17; one owning domain per event type, steps 3b and 5 of the
-- note and policy_alert split, taken in one file so PROD's migration Job runs
-- once for them). Each was a note with a field of its own:
--   external_work_noted (lineage): an external work's record on its stream
--     (was a note with external_work), written and checked by externalwork
--     from this release on;
--   plan_card_answered_by_rule, plan_card_settled, plan_observation,
--   plan_observation_pass, plan_step_marked_done, plan_view_adopted (plan):
--     the plan's records that are notes today; the plan declares their names
--     now and moves their writers in later changes, which need no migration;
--   action_traced (foundation): an action's trace on the stream action-trace
--     (was a note), written and declared by actionrecord from this release on.
-- The old rows stay as they are and the readers read both.
--
-- The one statement widens event_type_closed: 0031's list and the eight
-- types. Nothing is read for change, changed or deleted. Upgrading: run this
-- before the platform that writes the new types; an older platform writes
-- the old notes. The ALTER's lock and statement are bounded by migrate.apply
-- (2s, 30s), and a timeout rolls the file back.
-- Way back: 0031's constraint, once no event of the eight types is left.

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
 'full_ci_noted',
 'external_work_noted','plan_card_answered_by_rule','plan_card_settled','plan_observation',
 'plan_observation_pass','plan_step_marked_done','plan_view_adopted','action_traced'));
