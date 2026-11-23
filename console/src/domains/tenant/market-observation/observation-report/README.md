# Queue history report adapter

queueHistoryReportOptions(hours) reuses the existing unfiltered queue-history raw
cache key, polling and retry policy, adding cancellation through the existing client.
queueHistoryReport(data, receivedAt) supplies the same portable ObservationReport
as other providers, without importing their domain models. Samples retain timestamp,
kind, raw interval deltas, missing values and seconds. No filled gaps, inferred live
health or counts-to-rate conversion. Response evaluation time is not supplied;
individual sample time is evidence only of that historical sample.

This exact-reading adapter does not replace QueueHistoryPanel's time-series charts
or its statistical definitions. No plugin ingestion controls or backend API changes.
