import type { InteractionUpdate as CoreInteractionUpdate } from "@anysphere/proto/agent/v1/agent_pb.js";
import type { InteractionUpdate as SdkInteractionUpdate } from "./delta-types.js";
export declare function coreToSdkUpdate(core: CoreInteractionUpdate, sessionId: string): SdkInteractionUpdate | null;
