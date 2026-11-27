package com.micov.wxxyshall.security

import android.content.Context
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.util.Base64
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.intPreferencesKey
import androidx.datastore.preferences.core.longPreferencesKey
import androidx.datastore.preferences.core.stringPreferencesKey
import androidx.datastore.preferences.preferencesDataStore
import com.micov.wxxyshall.domain.SchoolSession
import java.security.KeyStore
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec
import kotlinx.coroutines.flow.first

private const val KEY_ALIAS = "wxxyshall-school-token-v1"
private const val TRANSFORMATION = "AES/GCM/NoPadding"
private val Context.sessionDataStore by preferencesDataStore("school-session")

class SecureSessionStore(private val context: Context) {
    private object Keys {
        val token = stringPreferencesKey("token_cipher")
        val iv = stringPreferencesKey("token_iv")
        val refreshToken = stringPreferencesKey("refresh_token_cipher")
        val refreshIv = stringPreferencesKey("refresh_token_iv")
        val expiresIn = intPreferencesKey("expires_in")
        val loginTime = longPreferencesKey("login_time")
        val sno = stringPreferencesKey("sno")
    }

    suspend fun save(session: SchoolSession) {
        val token = encrypt(session.accessToken)
        val refresh = if (session.refreshToken.isBlank()) null else encrypt(session.refreshToken)
        context.sessionDataStore.edit { prefs ->
            prefs[Keys.token] = token.cipherText
            prefs[Keys.iv] = token.iv
            if (refresh == null) {
                prefs.remove(Keys.refreshToken)
                prefs.remove(Keys.refreshIv)
            } else {
                prefs[Keys.refreshToken] = refresh.cipherText
                prefs[Keys.refreshIv] = refresh.iv
            }
            prefs[Keys.expiresIn] = session.expiresIn
            prefs[Keys.loginTime] = session.loginTime
            prefs[Keys.sno] = session.sno
        }
    }

    suspend fun load(): SchoolSession? {
        val prefs = context.sessionDataStore.data.first()
        val cipherText = prefs[Keys.token] ?: return null
        val iv = prefs[Keys.iv] ?: return null
        return runCatching {
            SchoolSession(
                accessToken = decrypt(cipherText, iv),
                refreshToken = prefs[Keys.refreshToken]?.let { encrypted ->
                    prefs[Keys.refreshIv]?.let { decrypt(encrypted, it) }
                }.orEmpty(),
                expiresIn = prefs[Keys.expiresIn] ?: 0,
                loginTime = prefs[Keys.loginTime] ?: 0,
                sno = prefs[Keys.sno].orEmpty(),
            )
        }.getOrNull()
    }

    suspend fun clear() {
        context.sessionDataStore.edit { it.clear() }
    }

    private fun encrypt(value: String): EncryptedValue {
        val cipher = Cipher.getInstance(TRANSFORMATION)
        cipher.init(Cipher.ENCRYPT_MODE, secretKey())
        val encrypted = cipher.doFinal(value.toByteArray(Charsets.UTF_8))
        return EncryptedValue(
            cipherText = Base64.encodeToString(encrypted, Base64.NO_WRAP),
            iv = Base64.encodeToString(cipher.iv, Base64.NO_WRAP),
        )
    }

    private fun decrypt(cipherText: String, iv: String): String {
        val cipher = Cipher.getInstance(TRANSFORMATION)
        cipher.init(
            Cipher.DECRYPT_MODE,
            secretKey(),
            GCMParameterSpec(128, Base64.decode(iv, Base64.NO_WRAP)),
        )
        return cipher.doFinal(Base64.decode(cipherText, Base64.NO_WRAP)).toString(Charsets.UTF_8)
    }

    private fun secretKey(): SecretKey {
        val keyStore = KeyStore.getInstance("AndroidKeyStore").apply { load(null) }
        (keyStore.getKey(KEY_ALIAS, null) as? SecretKey)?.let { return it }
        val generator = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, "AndroidKeyStore")
        generator.init(
            KeyGenParameterSpec.Builder(
                KEY_ALIAS,
                KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT,
            )
                .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
                .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
                .setRandomizedEncryptionRequired(true)
                .build(),
        )
        return generator.generateKey()
    }

    private data class EncryptedValue(val cipherText: String, val iv: String)
}
