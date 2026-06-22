package com.stalkerhek.app.persistence

import android.content.Context
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.stringPreferencesKey
import androidx.datastore.preferences.preferencesDataStore
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.map
import kotlinx.serialization.json.Json

private val Context.dataStore by preferencesDataStore(name = "stalkerhek_settings")

class DataStoreRepository(private val context: Context) {
    private val json = Json { ignoreUnknownKeys = true }

    companion object {
        private val KEY_PROFILES = stringPreferencesKey("cached_profiles")
    }

    suspend fun saveProfilesJson(data: String) {
        context.dataStore.edit { prefs ->
            prefs[KEY_PROFILES] = data
        }
    }

    suspend fun loadProfilesJson(): String? {
        return context.dataStore.data.map { prefs ->
            prefs[KEY_PROFILES]
        }.first()
    }
}
