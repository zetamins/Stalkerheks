package com.stalkerhek.app.engine

import android.content.Context
import android.os.Build
import android.util.Log
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.decodeFromString
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonPrimitive

sealed class EngineState {
    data object Uninitialized : EngineState()
    data object Initializing : EngineState()
    data class Ready(val profilesLoaded: Int) : EngineState()
    data class Error(val message: String) : EngineState()
}

@Serializable
data class ProfileConfig(
    val id: Int = 0,
    val name: String = "",
    // Go's nativeGetProfiles emits "portalUrl" (camelCase); must match or the
    // field decodes empty.
    @SerialName("portalUrl") val portalUrl: String = "",
    val mac: String = "",
    val username: String = "",
    val password: String = "",
    @SerialName("hls_port") val hlsPort: Int = 0,
    @SerialName("proxy_port") val proxyPort: Int = 0,
    val timezone: String = "UTC",
    @SerialName("serial_number") val serialNumber: String = "0000000000000",
    @SerialName("device_id") val deviceId: String = "f".repeat(64),
    @SerialName("device_id2") val deviceId2: String = "f".repeat(64),
    val signature: String = "f".repeat(64),
    val model: String = "MAG254",
    @SerialName("watchdog_interval") val watchdogInterval: Int = 5,
    @SerialName("device_id_auth") val deviceIdAuth: Boolean = true,
    @SerialName("hls_enabled") val hlsEnabled: Boolean = true,
    @SerialName("proxy_enabled") val proxyEnabled: Boolean = true,
    @SerialName("proxy_rewrite") val proxyRewrite: Boolean = true,
)

@Serializable
data class ProfileStatus(
    val phase: String = "idle",
    val message: String = "Not started",
    @SerialName("channels_count") val channelsCount: Int = 0,
    @SerialName("hls_addr") val hlsAddr: String = "",
    @SerialName("proxy_addr") val proxyAddr: String = "",
    val running: Boolean = false,
)

@Serializable
data class Channel(
    val cmd: String = "",
    val title: String = "",
    val genre: String = "",
    val genreId: String = "",
    val logo: String = "",
    val enabled: Boolean = true,
)

object EngineController {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    private val json = Json { ignoreUnknownKeys = true; encodeDefaults = true }

    private val _engineState = MutableStateFlow<EngineState>(EngineState.Uninitialized)
    val engineState: StateFlow<EngineState> = _engineState.asStateFlow()

    private val _profiles = MutableStateFlow<List<ProfileConfig>>(emptyList())
    val profiles: StateFlow<List<ProfileConfig>> = _profiles.asStateFlow()

    private val _activeProfile = MutableStateFlow<ProfileStatus?>(null)
    val activeProfile: StateFlow<ProfileStatus?> = _activeProfile.asStateFlow()

    private val _activeProfileId = MutableStateFlow(0)
    val activeProfileId: StateFlow<Int> = _activeProfileId.asStateFlow()

    private var initCalled = false
    private var lastDataDir: String? = null
    private var lastContext: Context? = null

    // Child process for the non-arm64 exec fallback; kept so shutdown()/restart()
    // can actually stop it instead of leaking it.
    private var execProcess: Process? = null

    fun init(dataDir: String, appContext: Context? = null) {
        if (initCalled) return
        initCalled = true
        lastDataDir = dataDir
        lastContext = appContext
        _engineState.value = EngineState.Initializing
        scope.launch {
            try {
                val isArm64 = Build.SUPPORTED_ABIS.isNotEmpty() && Build.SUPPORTED_ABIS[0].contains("arm64")
                if (isArm64) {
                    Log.i("Stalkerhek", "JNI init: $dataDir")
                    val result = EngineBridge.nativeInit(dataDir)
                    Log.i("Stalkerhek", "JNI result: $result")
                    val initResp = json.decodeFromString<Map<String, JsonElement>>(result)
                    val ok = (initResp["ok"] as? kotlinx.serialization.json.JsonPrimitive)?.content == "true"
                        || initResp["ok"]?.toString() == "true"
                    val loaded = initResp["profiles_loaded"]?.toString()?.toIntOrNull() ?: 0
                    if (ok) {
                        _engineState.value = EngineState.Ready(loaded)
                        refreshProfiles()
                        // Auto-start the first profile if one exists and none is running
                        if (_activeProfile.value?.running != true && _profiles.value.isNotEmpty()) {
                            val first = _profiles.value.first()
                            Log.i("Stalkerhek", "Auto-starting profile: ${first.name}")
                            val result = startProfile(first)
                            result.onSuccess { s ->
                                Log.i("Stalkerhek", "Auto-start OK: running=${s.running} proxy=${s.proxyAddr} hls=${s.hlsAddr}")
                            }.onFailure { e ->
                                Log.e("Stalkerhek", "Auto-start FAILED: ${e.message}")
                            }
                        }
                    } else {
                        val err = initResp["error"]?.toString() ?: "Init failed"
                        _engineState.value = EngineState.Error(err)
                    }
                } else {
                    Log.i("Stalkerhek", "Exec init: $dataDir")
                    val ctx = appContext ?: throw IllegalStateException("Context required for exec fallback")
                    val binPath = ctx.applicationInfo.nativeLibraryDir + "/libstalkerhek.so"
                    try { execProcess?.destroy() } catch (_: Exception) {}
                    execProcess = Runtime.getRuntime().exec(arrayOf(binPath, "-profile", "default", "-db", dataDir))
                    kotlinx.coroutines.delay(3000)
                    _engineState.value = EngineState.Ready(0)
                }
            } catch (e: Exception) {
                Log.e("Stalkerhek", "Engine init failed", e)
                _engineState.value = EngineState.Error(e.message ?: "Unknown error")
            }
        }
    }

    /** Restart the engine — shutdown and reinitialize. */
    fun restart() {
        val dir = lastDataDir ?: return
        val ctx = lastContext
        scope.launch {
            try { EngineBridge.nativeShutdown() } catch (_: Exception) {}
            _activeProfile.value = null
            _activeProfileId.value = 0
            _profiles.value = emptyList()
            initCalled = false
            init(dir, ctx)
        }
    }

    fun shutdown() {
        try { EngineBridge.nativeShutdown() } catch (_: Exception) {}
        try { execProcess?.destroy() } catch (_: Exception) {}
        execProcess = null
    }

    /** Public refresh — call from UI polling to pick up dashboard changes. */
    suspend fun refresh() {
        refreshProfiles()
    }

    suspend fun startProfile(profile: ProfileConfig): Result<ProfileStatus> {
        return try {
            val result = EngineBridge.nativeStartProfile(json.encodeToString(profile))
            Log.i("Stalkerhek", "nativeStartProfile response: $result")
            val status = json.decodeFromString<ProfileStatus>(result)
            if (status.running) {
                _activeProfileId.value = profile.id
                _activeProfile.value = status
            }
            refreshProfiles()
            Result.success(status)
        } catch (e: Exception) {
            Result.failure(e)
        }
    }

    suspend fun stopProfile(id: Int): Result<Unit> {
        return try {
            EngineBridge.nativeStopProfile(id)
            if (_activeProfileId.value == id) {
                _activeProfile.value = null
                _activeProfileId.value = 0
            }
            refreshProfiles()
            Result.success(Unit)
        } catch (e: Exception) {
            Result.failure(e)
        }
    }

    suspend fun getChannels(profileId: Int, type: String = "itv"): List<Channel> {
        return try {
            val result = EngineBridge.nativeGetChannels(profileId, type)
            json.decodeFromString<List<Channel>>(result)
        } catch (_: Exception) { emptyList() }
    }

    suspend fun getProfileStatus(profileId: Int): ProfileStatus? {
        return try {
            val result = EngineBridge.nativeGetProfileStatus(profileId)
            json.decodeFromString<ProfileStatus>(result)
        } catch (_: Exception) { null }
    }

    suspend fun createProfile(profile: ProfileConfig): Result<ProfileConfig> {
        return try {
            val result = EngineBridge.nativeCreateProfile(json.encodeToString(profile))
            val created = json.decodeFromString<ProfileConfig>(result)
            refreshProfiles()
            Result.success(created)
        } catch (e: Exception) { Result.failure(e) }
    }

    suspend fun deleteProfile(id: Int): Result<Unit> {
        return try {
            EngineBridge.nativeDeleteProfile(id)
            refreshProfiles()
            Result.success(Unit)
        } catch (e: Exception) { Result.failure(e) }
    }

    private suspend fun refreshProfiles() {
        try {
            val result = EngineBridge.nativeGetProfiles()
            _profiles.value = json.decodeFromString<List<ProfileConfig>>(result)
            for (p in _profiles.value) {
                val status = getProfileStatus(p.id)
                if (status?.running == true) {
                    _activeProfileId.value = p.id
                    _activeProfile.value = status
                    break
                }
            }
        } catch (_: Exception) {}
    }
}
