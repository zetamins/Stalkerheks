package com.stalkerhek.app.engine;

/**
 * JNI bridge to the native stalkerhek Go engine compiled as a shared library.
 * The .so is bundled in jniLibs/<abi>/libstalkerhek_engine.so and loaded via
 * System.loadLibrary at class init time.
 */
public class EngineBridge {
    static { System.loadLibrary("stalkerhek_engine"); }

    /** Initialize engine with data directory. Returns JSON: {"ok":"true","profiles_loaded":N} */
    public static native String nativeInit(String dataDir);

    /** Start a profile. Accepts ProfileConfig JSON, returns ProfileStatus JSON. */
    public static native String nativeStartProfile(String profileJson);

    /** Stop a profile by ID. Returns {"ok":"true"} or {"ok":"false"}. */
    public static native String nativeStopProfile(int profileId);

    /** Get channels for a profile by media type ("itv","vod","series"). Returns JSON array. */
    public static native String nativeGetChannels(int profileId, String mediaType);

    /** Get all profiles. Returns JSON array of ProfileConfig. */
    public static native String nativeGetProfiles();

    /** Get profile status by ID. Returns ProfileStatus JSON. */
    public static native String nativeGetProfileStatus(int profileId);

    /** Create or update a profile. Returns ProfileConfig JSON. */
    public static native String nativeCreateProfile(String profileJson);

    /** Delete a profile by ID. */
    public static native String nativeDeleteProfile(int profileId);

    /** Gracefully shutdown engine. */
    public static native String nativeShutdown();
}
