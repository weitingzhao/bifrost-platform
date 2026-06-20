type FeatureGateCheckOptions = {
    disableExposureLog?: boolean;
};
export interface SdkStatsigClient {
    checkFeatureGate(gateName: string, options?: FeatureGateCheckOptions): boolean;
    getDynamicConfigValue<T>(params: {
        configName: string;
        paramName: string;
        defaultValue: T;
    }): T;
}
export declare function bootstrapSdkStatsig(apiKey: string): Promise<SdkStatsigClient>;
export {};
//# sourceMappingURL=sdk-statsig.d.ts.map