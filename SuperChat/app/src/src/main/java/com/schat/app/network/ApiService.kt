package com.schat.app.network

import com.schat.app.data.*
import okhttp3.MultipartBody
import retrofit2.http.*

/**
 * REST API 接口定义，与服务端 routes() 一一对应
 */
interface ApiService {

    // ---- 认证 ----
    @POST("auth/register")
    suspend fun register(@Body body: AuthRequest): ApiResponse<TokenData>

    @POST("auth/login")
    suspend fun login(@Body body: AuthRequest): ApiResponse<TokenData>

    @GET("me")
    suspend fun getMe(): ApiResponse<User>

    @GET("users")
    suspend fun searchUsers(@Query("q") query: String): ApiResponse<List<User>>

    // 通讯录：与我共处过任意会话的所有用户（群聊拉人候选）
    @GET("contacts")
    suspend fun getContacts(): ApiResponse<List<User>>

    // 会话详情（含完整成员列表）
    @GET("conversations/{id}")
    suspend fun getConversation(@Path("id") id: Long): ApiResponse<Conversation>

    @Multipart
    @POST("avatar")
    suspend fun uploadAvatar(@Part file: MultipartBody.Part): ApiResponse<User>

    // ---- 会话 ----
    @GET("conversations")
    suspend fun getConversations(): ApiResponse<List<Conversation>>

    @POST("conversations")
    suspend fun createConversation(@Body body: CreateConvRequest): ApiResponse<Conversation>

    @GET("conversations/{id}/messages")
    suspend fun getMessages(
        @Path("id") id: Long,
        @Query("before_id") beforeId: Long? = null,
        @Query("limit") limit: Int = 50
    ): ApiResponse<List<Message>>

    @POST("conversations/{id}/read")
    suspend fun markRead(@Path("id") id: Long): ApiResponse<Map<String, Any>>

    @POST("conversations/{id}/members")
    suspend fun addMembers(
        @Path("id") id: Long,
        @Body body: AddMembersRequest
    ): ApiResponse<Conversation>

    @DELETE("conversations/{id}/members/me")
    suspend fun leaveConversation(@Path("id") id: Long): ApiResponse<Map<String, Any>>

    // ---- 消息 ----
    @POST("messages")
    suspend fun sendMessage(@Body body: SendMessageRequest): ApiResponse<Message>

    // ---- 文件 ----
    @Multipart
    @POST("files")
    suspend fun uploadFile(@Part file: MultipartBody.Part): ApiResponse<FileInfo>
}
