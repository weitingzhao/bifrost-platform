import type { MatrixResponse } from '@/api/types'
import { BlueprintPhase1SignoffPanel } from '@/components/architecture/BlueprintPhase1SignoffPanel'
import { DevAgentPlatformSignoffPanel } from '@/components/architecture/DevAgentPlatformSignoffPanel'
import { GovernancePhase2SignoffPanel } from '@/components/architecture/GovernancePhase2SignoffPanel'
import { GovernancePhase3SignoffPanel } from '@/components/architecture/GovernancePhase3SignoffPanel'
import { GovernancePhase4SignoffPanel } from '@/components/architecture/GovernancePhase4SignoffPanel'
import { GovernancePhase5SignoffPanel } from '@/components/architecture/GovernancePhase5SignoffPanel'
import { GovernancePhase6SignoffPanel } from '@/components/architecture/GovernancePhase6SignoffPanel'
import { GovernancePhase7SignoffPanel } from '@/components/architecture/GovernancePhase7SignoffPanel'
import { GovernanceProgramStatusStrip } from '@/components/architecture/GovernanceProgramStatusStrip'
import { IbGatewayPluginHardeningSignoffPanel } from '@/components/architecture/IbGatewayPluginHardeningSignoffPanel'
import { IbGatewayPluginPhase0SignoffPanel } from '@/components/architecture/IbGatewayPluginPhase0SignoffPanel'
import { IbGatewayPluginPhase1SignoffPanel } from '@/components/architecture/IbGatewayPluginPhase1SignoffPanel'
import { IbGatewayPluginPhase2SignoffPanel } from '@/components/architecture/IbGatewayPluginPhase2SignoffPanel'
import { IbGatewayPluginPhase3SignoffPanel } from '@/components/architecture/IbGatewayPluginPhase3SignoffPanel'
import { IbGatewayPluginPhase4SignoffPanel } from '@/components/architecture/IbGatewayPluginPhase4SignoffPanel'
import { IbGatewayPluginProgramSignoffPanel } from '@/components/architecture/IbGatewayPluginProgramSignoffPanel'
import { IbGatewayPluginProgramStatusStrip } from '@/components/architecture/IbGatewayPluginProgramStatusStrip'
import { NetworkGovernancePhase1SignoffPanel } from '@/components/architecture/NetworkGovernancePhase1SignoffPanel'
import { NetworkGovernancePhase2SignoffPanel } from '@/components/architecture/NetworkGovernancePhase2SignoffPanel'
import { NetworkGovernancePhase3SignoffPanel } from '@/components/architecture/NetworkGovernancePhase3SignoffPanel'
import { NetworkGovernancePhase4SignoffPanel } from '@/components/architecture/NetworkGovernancePhase4SignoffPanel'
import { NetworkGovernancePhase5SignoffPanel } from '@/components/architecture/NetworkGovernancePhase5SignoffPanel'
import { NetworkGovernancePhase6SignoffPanel } from '@/components/architecture/NetworkGovernancePhase6SignoffPanel'
import { NetworkGovernancePhase7SignoffPanel } from '@/components/architecture/NetworkGovernancePhase7SignoffPanel'
import { NetworkGovernancePhase8SignoffPanel } from '@/components/architecture/NetworkGovernancePhase8SignoffPanel'
import { NetworkGovernanceProgramStatusStrip } from '@/components/architecture/NetworkGovernanceProgramStatusStrip'
import { TradeIbClientMigrationPhase0SignoffPanel } from '@/components/architecture/TradeIbClientMigrationPhase0SignoffPanel'
import { TradeIbClientMigrationPhase1SignoffPanel } from '@/components/architecture/TradeIbClientMigrationPhase1SignoffPanel'
import { TradeIbClientMigrationPhase2SignoffPanel } from '@/components/architecture/TradeIbClientMigrationPhase2SignoffPanel'
import { TradeIbClientMigrationPhase3SignoffPanel } from '@/components/architecture/TradeIbClientMigrationPhase3SignoffPanel'
import { TradeIbClientMigrationPhase4SignoffPanel } from '@/components/architecture/TradeIbClientMigrationPhase4SignoffPanel'
import { TradeIbClientMigrationProgramSignoffPanel } from '@/components/architecture/TradeIbClientMigrationProgramSignoffPanel'
import { TradeIbClientMigrationProgramStatusStrip } from '@/components/architecture/TradeIbClientMigrationProgramStatusStrip'
import { TradeIbClientMigrationRolloutDevComposeSignoffPanel } from '@/components/architecture/TradeIbClientMigrationRolloutDevComposeSignoffPanel'
import { TradeIbClientMigrationRolloutProdSignoffPanel } from '@/components/architecture/TradeIbClientMigrationRolloutProdSignoffPanel'
import { TradeIbClientMigrationRolloutStgSignoffPanel } from '@/components/architecture/TradeIbClientMigrationRolloutStgSignoffPanel'
import { TradeIbClientMigrationRolloutW1SignoffPanel } from '@/components/architecture/TradeIbClientMigrationRolloutW1SignoffPanel'
import { TradeIbClientMigrationRolloutW2SignoffPanel } from '@/components/architecture/TradeIbClientMigrationRolloutW2SignoffPanel'
import { TradeIbClientMigrationRolloutW3SignoffPanel } from '@/components/architecture/TradeIbClientMigrationRolloutW3SignoffPanel'
import { UnifiMcpServerPhase1SignoffPanel } from '@/components/architecture/UnifiMcpServerPhase1SignoffPanel'
import { UnifiMcpServerPhase2SignoffPanel } from '@/components/architecture/UnifiMcpServerPhase2SignoffPanel'
import { UnifiMcpServerPhase3SignoffPanel } from '@/components/architecture/UnifiMcpServerPhase3SignoffPanel'
import { UnifiMcpServerPhase4SignoffPanel } from '@/components/architecture/UnifiMcpServerPhase4SignoffPanel'
import { UnifiMcpServerProgramStatusStrip } from '@/components/architecture/UnifiMcpServerProgramStatusStrip'
import { VisionS3GatePanel } from '@/components/architecture/VisionS3GatePanel'
import { VisionV1GatePanel } from '@/components/architecture/VisionV1GatePanel'
import { VisionV2GatePanel } from '@/components/architecture/VisionV2GatePanel'
import { VisionV3GatePanel } from '@/components/architecture/VisionV3GatePanel'
import { VisionV4GatePanel } from '@/components/architecture/VisionV4GatePanel'
import { VisionV5GatePanel } from '@/components/architecture/VisionV5GatePanel'
import { BriefingPhase1SignoffPanel } from '@/components/briefing/BriefingPhase1SignoffPanel'
import { BriefingPhase2SignoffPanel } from '@/components/briefing/BriefingPhase2SignoffPanel'
import { BriefingPhase3SignoffPanel } from '@/components/briefing/BriefingPhase3SignoffPanel'
import { BriefingPhase4SignoffPanel } from '@/components/briefing/BriefingPhase4SignoffPanel'
import { BriefingRoadmapStatusStrip } from '@/components/briefing/BriefingRoadmapStatusStrip'
import { ControlRoomPhase0SignoffPanel } from '@/components/control-room/ControlRoomPhase0SignoffPanel'
import { ControlRoomPhase1SignoffPanel } from '@/components/control-room/ControlRoomPhase1SignoffPanel'
import { ControlRoomPhase2SignoffPanel } from '@/components/control-room/ControlRoomPhase2SignoffPanel'
import { ControlRoomPhase3SignoffPanel } from '@/components/control-room/ControlRoomPhase3SignoffPanel'
import { ControlRoomPhase4SignoffPanel } from '@/components/control-room/ControlRoomPhase4SignoffPanel'
import { ControlRoomPhase6SignoffPanel } from '@/components/control-room/ControlRoomPhase6SignoffPanel'
import { ControlRoomPhase5SignoffPanel } from '@/components/control-room/ControlRoomPhase5SignoffPanel'
import { ControlRoomProgramStatusStrip } from '@/components/control-room/ControlRoomProgramStatusStrip'
import { MissionSignalPhase1SignoffPanel } from '@/components/control-room/MissionSignalPhase1SignoffPanel'
import { MissionSignalPhase2SignoffPanel } from '@/components/control-room/MissionSignalPhase2SignoffPanel'
import { MissionSignalPhase3SignoffPanel } from '@/components/control-room/MissionSignalPhase3SignoffPanel'
import { MissionSignalPhase4SignoffPanel } from '@/components/control-room/MissionSignalPhase4SignoffPanel'
import { MissionSignalPhase5SignoffPanel } from '@/components/control-room/MissionSignalPhase5SignoffPanel'
import { MissionSignalPhase6SignoffPanel } from '@/components/control-room/MissionSignalPhase6SignoffPanel'
import { MissionSignalPhase7SignoffPanel } from '@/components/control-room/MissionSignalPhase7SignoffPanel'
import { MissionSignalProgramStatusStrip } from '@/components/control-room/MissionSignalProgramStatusStrip'
import type { DeliveryBoardProgramId } from '@/lib/delivery/deliveryBoardPrograms'

export function DeliveryBoardProgramPanels({
  programId,
  matrices,
}: {
  programId: DeliveryBoardProgramId
  matrices: MatrixResponse[]
}) {
  switch (programId) {
    case 'mission-signal':
      return (
        <div className="flex flex-col gap-4">
          <MissionSignalProgramStatusStrip />
          <MissionSignalPhase7SignoffPanel />
          <MissionSignalPhase6SignoffPanel />
          <MissionSignalPhase5SignoffPanel />
          <MissionSignalPhase4SignoffPanel />
          <MissionSignalPhase3SignoffPanel />
          <MissionSignalPhase2SignoffPanel />
          <MissionSignalPhase1SignoffPanel matrices={matrices} />
        </div>
      )
    case 'control-room-ui':
      return (
        <div className="flex flex-col gap-4">
          <ControlRoomProgramStatusStrip />
          <ControlRoomPhase6SignoffPanel />
          <ControlRoomPhase5SignoffPanel />
          <ControlRoomPhase4SignoffPanel />
          <ControlRoomPhase3SignoffPanel />
          <ControlRoomPhase2SignoffPanel />
          <ControlRoomPhase1SignoffPanel />
          <ControlRoomPhase0SignoffPanel />
        </div>
      )
    case 'governance':
      return (
        <div className="flex flex-col gap-4">
          <GovernanceProgramStatusStrip />
          <BlueprintPhase1SignoffPanel />
          <GovernancePhase2SignoffPanel />
          <GovernancePhase3SignoffPanel />
          <GovernancePhase4SignoffPanel />
          <GovernancePhase5SignoffPanel />
          <GovernancePhase6SignoffPanel />
          <GovernancePhase7SignoffPanel />
        </div>
      )
    case 'network-governance':
      return (
        <div className="flex flex-col gap-4">
          <NetworkGovernanceProgramStatusStrip />
          <NetworkGovernancePhase8SignoffPanel />
          <NetworkGovernancePhase7SignoffPanel />
          <NetworkGovernancePhase6SignoffPanel />
          <NetworkGovernancePhase5SignoffPanel />
          <NetworkGovernancePhase4SignoffPanel />
          <NetworkGovernancePhase3SignoffPanel />
          <NetworkGovernancePhase2SignoffPanel />
          <NetworkGovernancePhase1SignoffPanel />
        </div>
      )
    case 'trade-ib-migration':
      return (
        <div className="flex flex-col gap-4">
          <TradeIbClientMigrationProgramStatusStrip />
          <TradeIbClientMigrationRolloutProdSignoffPanel />
          <TradeIbClientMigrationRolloutDevComposeSignoffPanel />
          <TradeIbClientMigrationRolloutStgSignoffPanel />
          <TradeIbClientMigrationRolloutW3SignoffPanel />
          <TradeIbClientMigrationRolloutW2SignoffPanel />
          <TradeIbClientMigrationRolloutW1SignoffPanel />
          <TradeIbClientMigrationProgramSignoffPanel />
          <TradeIbClientMigrationPhase4SignoffPanel />
          <TradeIbClientMigrationPhase3SignoffPanel />
          <TradeIbClientMigrationPhase2SignoffPanel />
          <TradeIbClientMigrationPhase1SignoffPanel />
          <TradeIbClientMigrationPhase0SignoffPanel />
        </div>
      )
    case 'ib-gateway-plugin':
      return (
        <div className="flex flex-col gap-4">
          <IbGatewayPluginProgramStatusStrip />
          <IbGatewayPluginHardeningSignoffPanel />
          <IbGatewayPluginProgramSignoffPanel />
          <IbGatewayPluginPhase4SignoffPanel />
          <IbGatewayPluginPhase3SignoffPanel />
          <IbGatewayPluginPhase2SignoffPanel />
          <IbGatewayPluginPhase1SignoffPanel />
          <IbGatewayPluginPhase0SignoffPanel />
        </div>
      )
    case 'unifi-mcp':
      return (
        <div className="flex flex-col gap-4">
          <UnifiMcpServerProgramStatusStrip />
          <UnifiMcpServerPhase4SignoffPanel />
          <UnifiMcpServerPhase3SignoffPanel />
          <UnifiMcpServerPhase2SignoffPanel />
          <UnifiMcpServerPhase1SignoffPanel />
        </div>
      )
    case 'briefing':
      return (
        <div className="flex flex-col gap-4">
          <BriefingRoadmapStatusStrip />
          <BriefingPhase4SignoffPanel />
          <BriefingPhase3SignoffPanel />
          <BriefingPhase2SignoffPanel />
          <BriefingPhase1SignoffPanel />
        </div>
      )
    case 'dev-agent':
      return (
        <div className="flex flex-col gap-4">
          <DevAgentPlatformSignoffPanel />
        </div>
      )
    case 'vision':
      return (
        <div className="flex flex-col gap-4">
          <VisionV5GatePanel />
          <VisionV4GatePanel />
          <VisionV3GatePanel />
          <VisionV2GatePanel />
          <VisionS3GatePanel />
          <VisionV1GatePanel />
        </div>
      )
    default:
      return null
  }
}
