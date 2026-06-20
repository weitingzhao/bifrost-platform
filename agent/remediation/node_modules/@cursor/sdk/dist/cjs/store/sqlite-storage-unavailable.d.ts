import { ConfigurationError } from "../errors.js";
export declare const SQLITE_STORAGE_UNAVAILABLE_MESSAGE: string;
export declare function isSqliteModuleLoadError(err: unknown): boolean;
export declare function createSqliteStorageUnavailableError(cause?: unknown): ConfigurationError;
export declare function rethrowIfSqliteModuleLoadError(err: unknown): never;
//# sourceMappingURL=sqlite-storage-unavailable.d.ts.map