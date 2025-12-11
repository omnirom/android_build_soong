package dexpreopt

import "android/soong/android"

func init() {
	android.InitRegistrationContext.RegisterParallelSingletonType("system_server_zip_singleton", systemServerZipSingletonFactory)
}

func systemServerZipSingletonFactory() android.Singleton {
	return &systemServerZipSingleton{}
}

type systemServerZipSingleton struct{}

func (s *systemServerZipSingleton) GenerateBuildActions(ctx android.SingletonContext) {
	global := GetGlobalConfig(ctx)
	if global.DisablePreopt || global.OnlyPreoptArtBootImage {
		return
	}

	systemServerDexjarsDir := android.PathForOutput(ctx, SystemServerDexjarsDir)

	out := android.PathForOutput(ctx, "system_server.zip")
	builder := android.NewRuleBuilder(pctx, ctx)
	cmd := builder.Command().BuiltTool("soong_zip").
		FlagWithOutput("-o ", out).
		FlagWithArg("-C ", systemServerDexjarsDir.String())

	for i := range global.SystemServerJars.Len() {
		jar := global.SystemServerJars.Jar(i) + ".jar"
		cmd.FlagWithInput("-f ", systemServerDexjarsDir.Join(ctx, jar))
	}
	for i := range global.StandaloneSystemServerJars.Len() {
		jar := global.StandaloneSystemServerJars.Jar(i) + ".jar"
		cmd.FlagWithInput("-f ", systemServerDexjarsDir.Join(ctx, jar))
	}
	for i := range global.ApexSystemServerJars.Len() {
		jar := global.ApexSystemServerJars.Jar(i) + ".jar"
		cmd.FlagWithInput("-f ", systemServerDexjarsDir.Join(ctx, jar))
	}
	for i := range global.ApexStandaloneSystemServerJars.Len() {
		jar := global.ApexStandaloneSystemServerJars.Jar(i) + ".jar"
		cmd.FlagWithInput("-f ", systemServerDexjarsDir.Join(ctx, jar))
	}

	builder.Build("system_server_zip", "building system_server.zip")

	ctx.DistForGoal("droidcore", out)
}
