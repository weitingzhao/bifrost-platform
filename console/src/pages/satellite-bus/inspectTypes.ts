import type {
  BusAttentionIssue,
  BusConsumerRow,
  BusPathNode,
} from '@/lib/satellite-bus/satelliteBusViewModel'

export type InspectTarget =
  | { kind: 'node'; node: BusPathNode }
  | { kind: 'consumer'; row: BusConsumerRow }
  | { kind: 'issue'; issue: BusAttentionIssue }
