package com.schat.app.network

import com.google.gson.Gson
import com.google.gson.JsonObject
import com.schat.app.data.*
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.SharedFlow
import okhttp3.MediaType.Companion.toMediaTypeOrNull
import okhttp3.MultipartBody
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import retrofit2.HttpException
import retrofit2.Retrofit
import retrofit2.converter.gson.GsonConverterFactory
import java.util.concurrent.TimeUnit

/**
 * 网络客户端：HTTP(Retrofit) + WebSocket(OkHttp)
 *
 * 生命周期：
 * - setToken() 设置 token 后所有请求自动带 Authorization 头
 * - connectWebSocket() 启动 WS 连接和心跳
 * - disconnectWebSocket() 停止 WS、心跳、重连
 */
object ApiClient {

    private const val BASE_URL = "https://asdaczsda.xyz:8443/api/"
    private const val FILE_BASE = "https://asdaczsda.xyz:8443"
    private const val WS_URL = "wss://asdaczsda.xyz:8443/api/ws"

    @Volatile
    private var token: String? = null

    private val gson = Gson()

    private val httpClient = OkHttpClient.Builder()
        .connectTimeout(8, TimeUnit.SECONDS)   // TCP 连接超时：断网时 8 秒放弃
        .readTimeout(30, TimeUnit.SECONDS)     // 读取超时：服务端响应慢
        .writeTimeout(60, TimeUnit.SECONDS)    // 写入超时：上传大文件
        .callTimeout(15, TimeUnit.SECONDS)     // 整条请求兜底：DNS 卡住时 15 秒强制结束
        .addInterceptor { chain ->
            val builder = chain.request().newBuilder()
            token?.let { builder.addHeader("Authorization", "Bearer $it") }
            chain.proceed(builder.build())
        }
        .build()

    private val retrofit = Retrofit.Builder()
        .baseUrl(BASE_URL)
        .client(httpClient)
        .addConverterFactory(GsonConverterFactory.create())
        .build()

    val api: ApiService = retrofit.create(ApiService::class.java)

    // ---- Token ----
    fun setToken(t: String) { token = t }
    fun getToken(): String? = token
    fun clearToken() { token = null }

    // ---- 错误解析：从 HttpException 提取服务端错误信息 ----
    fun parseError(e: Throwable): String {
        return try {
            if (e is HttpException) {
                val body = e.response()?.errorBody()?.string()
                if (body != null) {
                    val obj = gson.fromJson(body, JsonObject::class.java)
                    obj.get("error")?.asString ?: "HTTP ${e.code()}"
                } else "HTTP ${e.code()}"
            } else "网络错误: ${e.message ?: "连接失败"}"
        } catch (_: Exception) {
            if (e is HttpException) "HTTP ${e.code()}" else "网络错误"
        }
    }

    // ---- 文件 URL ----
    fun fullFileUrl(path: String): String {
        val p = path.removePrefix("/")
        return if (token != null) "$FILE_BASE/$p?token=$token" else "$FILE_BASE/$p"
    }

    // ---- 头像 URL ----
    fun avatarUrl(avatarPath: String): String {
        if (avatarPath.isBlank()) return ""
        return fullFileUrl(avatarPath)
    }

    // ---- 头像上传 ----
    suspend fun uploadAvatar(bytes: ByteArray, fileName: String, mime: String): ApiResponse<User> {
        val body = RequestBody.create(mime.toMediaTypeOrNull(), bytes)
        val part = MultipartBody.Part.createFormData("file", fileName, body)
        return api.uploadAvatar(part)
    }

    // ---- 文件上传 ----
    suspend fun uploadFile(bytes: ByteArray, fileName: String, mime: String): ApiResponse<FileInfo> {
        val mediaType = mime.toMediaTypeOrNull()
        val body = RequestBody.create(mediaType, bytes)
        val part = MultipartBody.Part.createFormData("file", fileName, body)
        return api.uploadFile(part)
    }

    // ---- WebSocket ----
    private var ws: WebSocket? = null
    private var reconnectJob: Job? = null
    private var heartbeatJob: Job? = null
    private val wsScope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    @Volatile private var shouldConnect = false

    val wsEvents = MutableSharedFlow<WSEvent>(extraBufferCapacity = 64)

    fun connectWebSocket() {
        if (token == null) return
        shouldConnect = true
        disconnectWebSocket()
        shouldConnect = true
        doConnect()
    }

    private fun doConnect() {
        if (token == null || !shouldConnect) return

        val request = Request.Builder()
            .url("$WS_URL?token=$token")
            .build()

        ws = httpClient.newWebSocket(request, object : WebSocketListener() {
            override fun onOpen(webSocket: WebSocket, response: Response) {
                android.util.Log.d("SuperChat", "WS connected")
            }

            override fun onMessage(webSocket: WebSocket, text: String) {
                try {
                    val obj = gson.fromJson(text, JsonObject::class.java)
                    val type = obj.get("type")?.asString ?: return
                    val data = obj.get("data")
                    val event = when (type) {
                        "message" -> WSEvent("message", gson.fromJson(data, Message::class.java))
                        "conversation" -> WSEvent("conversation", gson.fromJson(data, Conversation::class.java))
                        "pong" -> WSEvent("pong", null)
                        else -> WSEvent(type, null)
                    }
                    wsEvents.tryEmit(event)
                } catch (e: Exception) {
                    android.util.Log.e("SuperChat", "WS parse error", e)
                }
            }

            override fun onClosed(webSocket: WebSocket, code: Int, reason: String) {
                android.util.Log.d("SuperChat", "WS closed: $reason")
                scheduleReconnect()
            }

            override fun onFailure(webSocket: WebSocket, t: Throwable, response: Response?) {
                android.util.Log.e("SuperChat", "WS failure: ${t.message}")
                scheduleReconnect()
            }
        })

        startHeartbeat()
    }

    /** 协程方式延迟重连，避免在回调线程 sleep */
    private fun scheduleReconnect() {
        if (!shouldConnect) return
        reconnectJob?.cancel()
        reconnectJob = wsScope.launch {
            delay(5000)
            if (shouldConnect) doConnect()
        }
    }

    /** 30 秒心跳 */
    private fun startHeartbeat() {
        heartbeatJob?.cancel()
        heartbeatJob = wsScope.launch {
            while (isActive) {
                delay(30_000)
                try {
                    ws?.send(gson.toJson(mapOf("type" to "ping")))
                } catch (e: Exception) {
                    break
                }
            }
        }
    }

    fun disconnectWebSocket() {
        shouldConnect = false
        reconnectJob?.cancel()
        heartbeatJob?.cancel()
        ws?.close(1000, "logout")
        ws = null
    }
}
