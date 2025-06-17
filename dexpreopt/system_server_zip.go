package dexpreopt

import "android/soong/android"

func init() {
	android.InitRegistrationContext.RegisterSingletonType("system_server_zip_singleton", systemServerZipSingletonFactory)
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

	for i := 0; i < global.SystemServerJars.Len(); i++ {
		jar := global.SystemServerJars.Jar(i) + ".jar"
		cmd.FlagWithInput("-f ", systemServerDexjarsDir.Join(ctx, jar))
	}
	for i := 0; i < global.StandaloneSystemServerJars.Len(); i++ {
		jar := global.StandaloneSystemServerJars.Jar(i) + ".jar"
		cmd.FlagWithInput("-f ", systemServerDexjarsDir.Join(ctx, jar))
	}
	for i := 0; i < global.ApexSystemServerJars.Len(); i++ {
		jar := global.ApexSystemServerJars.Jar(i) + ".jar"
		cmd.FlagWithInput("-f ", systemServerDexjarsDir.Join(ctx, jar))
	}
	for i := 0; i < global.ApexStandaloneSystemServerJars.Len(); i++ {
		jar := global.ApexStandaloneSystemServerJars.Jar(i) + ".jar"
		cmd.FlagWithInput("-f ", systemServerDexjarsDir.Join(ctx, jar))
	}

	builder.Build("system_server_zip", "building system_server.zip")

	ctx.DistForGoal("droidcore", out)
}
