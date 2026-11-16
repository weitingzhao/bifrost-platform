-- agentrt 0026: the whole rocket register in the database's closed
-- vocabulary (bus/RULES.md §7, step 2a of the communication layer,
-- 2026-11-16). One PROD migration for the register: every event type of
-- api/internal/bus/registry/rocket.yaml (36, git.main.advanced already in
-- 0025) is taken from here on, so a bus type that a publisher starts to write
-- later (gate-cluster's ci.run.started, ci.run.finished and
-- release.window.changed first) needs no further DDL. A type the register
-- adds after this file still needs its own migration; one the register drops
-- stays taken until a later file narrows the list.
--
-- The one statement widens event_type_closed: 0025's list and the 35
-- registered types it does not have, in register order. Nothing is read for
-- change, changed or deleted; the check is validated against every event when
-- it is added, and every event already passes the narrower list. One
-- statement, so the constraint is never missing between two.
--
-- Upgrading: run this before the platform that writes the new types. An
-- older platform still starts and writes none of them. A newer platform on a
-- database without this file starts as well; gate-cluster's writes are refused
-- (it logs and counts the error) until this file runs. The ALTER takes a
-- strong lock on event while it checks the rows; migrate.apply bounds the wait
-- (lock_timeout 2s) and the statement (30s), and a timeout rolls the file
-- back.
-- Way back: 0025's constraint, once no event of the added types is left.

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
 'dev.session.changed','reconcile.drift.found'));
