package com.schat.app.data

import com.google.gson.annotations.SerializedName

// ---- 认证 ----
data class AuthRequest(
    val username: String,
    val password: String,
    @SerializedName("display_name") val displayName: String? = null
)

data class TokenData(
    val token: String,
    val user: User
)

data class User(
    val id: Long,
    val username: String,
    @SerializedName("display_name") val displayName: String = "",
    @SerializedName("avatar_url") val avatarUrl: String = "",
    @SerializedName("created_at") val createdAt: Long = 0
)

// ---- 会话 ----
data class Conversation(
    val id: Long,
    val type: String, // "direct" | "group"
    val name: String = "",
    @SerializedName("owner_id") val ownerId: Long? = null,
    @SerializedName("created_at") val createdAt: Long = 0,
    val unread: Int = 0,
    @SerializedName("last_message") val lastMessage: Message? = null,
    val members: List<Member>? = null
)

data class Member(
    @SerializedName("user_id") val userId: Long,
    val username: String,
    @SerializedName("display_name") val displayName: String,
    @SerializedName("avatar_url") val avatarUrl: String = "",
    @SerializedName("joined_at") val joinedAt: Long = 0
)

// ---- 消息 ----
data class Message(
    val id: Long,
    @SerializedName("conversation_id") val conversationId: Long,
    @SerializedName("sender_id") val senderId: Long,
    @SerializedName("sender_name") val senderName: String = "",   // 发送者昵称（气泡旁头像用）
    @SerializedName("sender_avatar") val senderAvatar: String = "", // 发送者头像 URL
    val type: String, // "text" | "image" | "file"
    val content: String = "",
    @SerializedName("file_id") val fileId: Long? = null,
    val file: FileInfo? = null,
    @SerializedName("created_at") val createdAt: Long
)

// ---- 本地待发送消息（乐观 UI：发送中/失败重试，纯本地状态，不上传服务端） ----
data class PendingMsg(
    val tempId: Long,      // 本地负数递增 ID
    val content: String,
    val createdAt: Long,
    val status: Int        // PendingStatus.SENDING / FAILED
)

object PendingStatus {
    const val SENDING = 0  // 发送中：气泡旁小转圈
    const val FAILED = 1   // 发送失败：红色感叹号，点击气泡重试
}

data class FileInfo(
    val id: Long,
    val name: String,
    val url: String,
    val mime: String,
    val size: Long
)

// ---- API 请求体 ----
data class CreateConvRequest(
    val type: String,
    @SerializedName("user_id") val userId: Long? = null,
    val name: String? = null,
    @SerializedName("user_ids") val userIds: List<Long>? = null
)

data class SendMessageRequest(
    @SerializedName("conversation_id") val conversationId: Long,
    val type: String? = null,
    val content: String? = null,
    @SerializedName("file_id") val fileId: Long? = null
)

data class AddMembersRequest(
    @SerializedName("user_ids") val userIds: List<Long>
)

// ---- 通用响应 ----
data class ApiResponse<T>(
    val ok: Boolean,
    val data: T? = null,
    val error: String? = null
)

// ---- WebSocket 事件 ----
data class WSEvent(
    val type: String, // "message" | "conversation" | "pong"
    val data: Any? = null
)
