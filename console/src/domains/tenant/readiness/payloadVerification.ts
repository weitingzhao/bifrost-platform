/**
 * Payload verification — matrix vs cluster classification for Agent playbooks.
 * Mirrors GET /api/v1/mission/verify-payload (probe.VerifyPayload).
 */

export type PayloadClassification = 'NOMINAL' | 'PROBE_DRIFT' | 'DATA_LAYER' | 'HTTP_FAIL' | 'UNKNOWN'
