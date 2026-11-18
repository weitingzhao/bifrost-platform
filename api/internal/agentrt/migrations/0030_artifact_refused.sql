-- agentrt 0030: a refused report is its own event type (foundation line,
-- 2026-11-17; one owning domain per event type, step 2 of the note and
-- policy_alert split). A report agentd uploads that the platform refuses
-- (TD-382: a local path, or another task's workspace) was a policy_alert of
-- rule local_path or workspace_ref on the host's stream. It becomes
-- artifact_refused {rule, kind, artifact_id}, declared by the foundation's
-- artifact package (which holds the scans that refuse it) and registered under
-- foundation. The old rows stay as they are; a body recorded then is not
-- recorded again.
--
-- The one statement widens event_type_closed: 0029's list and the new type.
-- Nothing is read for change, changed or deleted; the check is validated
-- against every event when it is added, and every event already passes the
-- narrower list. Upgrading: run this before the platform that writes the new
-- type; an older platform still writes the old shape. The ALTER's lock and
-- statement are bounded by migrate.apply (2s, 30s), and a timeout rolls the
-- file back.
-- Way back: 0029's constraint, once no artifact_refused event is left.

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
 'artifact_refused'));
