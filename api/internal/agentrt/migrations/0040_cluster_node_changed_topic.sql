-- agentrt 0040: one bus topic, cluster.node.changed (LANE-C, 2026-11-20).
-- A node turned ready or not ready, or was cordoned or uncordoned. Origin: on
-- 2026-11-20 02:50Z ubt-k3s-05 was cordoned and drained for a disk swap; the
-- bus's NATS had its disk there and sat Pending for an hour, and nothing on the
-- bus said a node had changed (OWNER_LAW #37: the modules that stay are on the
-- bus).
--
-- The one statement widens event_type_closed: 0039's list and the one type.
-- Nothing is read for change, changed or deleted. Upgrading: run this before
-- the platform that writes the new type. The ALTER's lock and statement are
-- bounded by migrate.apply (2s, 30s), and a timeout rolls the file back.
-- Way back: 0039's constraint, once no event of the type is left.
ALTER TABLE agentrt.event DROP CONSTRAINT event_type_closed, ADD CONSTRAINT event_type_closed CHECK (event_type IN (
 'plan_node_created','plan_node_retitled','plan_node_state_changed','plan_change_proposed',
 'plan_change_decided','plan_changed','mission_proposed','mission_state_changed',
 'mission_amended','task_proposed','task_state_changed','task_dependency_added',
 'task_cancelled','route_decided','attempt_leased','attempt_started','attempt_progress',
 'attempt_checkpointed','attempt_stale','attempt_renewed','attempt_computed',
 'attempt_persisted','attempt_submitted','attempt_lost','attempt_ended','attempt_salvaged',
 'attempt_cancelled','artifact_put','verification_recorded','owner_wait_opened',
 'owner_wait_closed','approval_seen','question_seen','ci_seen','pipeline_seen',
 'deploy_seen','owner_signoff','actor_registered','host_registered','host_state_changed',
 'host_retired','control_set','control_cleared','rule_proposed','rule_signed',
 'rule_tightened','rule_expired','rule_approved','capability_scored',
 'candidate_status_changed','budget_charged','pool_cooldown_set','pool_measured',
 'message_posted','message_acked','policy_alert','decision_recorded','host_slots_set',
 'wave_opened','wave_closed','wave_abandoned','note','wait_limit_passed','legacy_imported',
 'metrics_computed','metric_label_set','ci_reused','ci_red_classified','git.main.advanced',
 'git.mirror.settled','ci.run.started','ci.run.finished','ci.verdict.changed',
 'release.deploy.finished','release.gitops.changed','release.window.changed',
 'release.policy.expiring','approval.card.created','approval.card.decided',
 'approval.card.executed','owner.question.asked','owner.question.answered',
 'seat.mail.arrived','seat.status.changed','seat.liveness.changed','seat.session.changed',
 'alert.rule.fired','loop.health.changed','env.health.changed','plugin.status.changed',
 'schema.migration.applied','data.job.finished','audit.action.recorded',
 'reconcile.drift.found','controller_loop_failing','controller_loop_recovered',
 'artifact_refused','full_ci_noted','external_work_noted','plan_card_answered_by_rule',
 'plan_card_settled','plan_observation','plan_observation_pass','plan_step_marked_done',
 'plan_view_adopted','action_traced','main.regression.known','main.regression.fixed',
 'home_layout_saved','plan.tree.changed','cluster.node.changed'));
