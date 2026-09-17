package com.schat.app.ui

import android.app.Application
import android.content.Context
import android.net.ConnectivityManager
import android.net.NetworkCapabilities
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import com.schat.app.data.*
import com.schat.app.network.ApiClient
import kotlinx.coroutines.flow.*
import kotlinx.coroutines.launch

/**
 * 全局状态持有者。
 *
 * 消息去重策略（核心）：
 * 服务端发送消息后会通过 WebSocket 把同一条消息推送回所有成员（含发送者）。
 * 客户端两个来源都会收到消息：
 *   1. POST /api/messages 的 HTTP 响应
 *   2. WebSocket "message" 事件
 * 统一走 addMessageDedup() 按 message.id 去重，保证每条消息只显示一次。
 */
class AppViewModel(app: Application) : AndroidViewModel(app) {

    // ---- 初始化状态 ----
    private val _isInitializing = MutableStateFlow(true)
    val isInitializing: StateFlow<Boolean> = _isInitializing

    private val _isLoggedIn = MutableStateFlow(false)
    val isLoggedIn: StateFlow<Boolean> = _isLoggedIn

    private val _currentUser = MutableStateFlow<User?>(null)
    val currentUser: StateFlow<User?> = _currentUser

    // ---- 业务状态 ----
    private val _conversations = MutableStateFlow<List<Conversation>>(emptyList())
    val conversations: StateFlow<List<Conversation>> = _conversations

    private val _messages = MutableStateFlow<List<Message>>(emptyList())
    val messages: StateFlow<List<Message>> = _messages

    // 本地待发送消息（乐观 UI）：发送中/失败重试，发送成功后由服务端消息顶替
    private val _pendingMsgs = MutableStateFlow<List<PendingMsg>>(emptyList())
    val pendingMsgs: StateFlow<List<PendingMsg>> = _pendingMsgs
    private var tempMsgId = -1L

    private val _currentConvId = MutableStateFlow(0L)
    val currentConvId: StateFlow<Long> = _currentConvId

    private val _currentConvName = MutableStateFlow("")
    val currentConvName: StateFlow<String> = _currentConvName

    private val _isLoading = MutableStateFlow(false)
    val isLoading: StateFlow<Boolean> = _isLoading

    private val _error = MutableSharedFlow<String>(extraBufferCapacity = 8)
    val error: SharedFlow<String> = _error

    // ============================================================
    // 初始化：恢复登录态 + 订阅 WebSocket 事件
    // ============================================================
    init {
        // 恢复登录态
        viewModelScope.launch {
            val token = TokenStore.getToken(app)
            if (token != null) {
                ApiClient.setToken(token)
                try {
                    val resp = ApiClient.api.getMe()
                    if (resp.ok && resp.data != null) {
                        _currentUser.value = resp.data
                        _isLoggedIn.value = true
                        ApiClient.connectWebSocket()
                        loadConversations()
                    } else {
                        TokenStore.clearToken(app)
                    }
                } catch (e: Exception) {
                    // token 过期或网络异常，清除本地 token
                    TokenStore.clearToken(app)
                }
            }
            _isInitializing.value = false
        }

        // 订阅 WebSocket 推送（整个 App 生命周期只有一个订阅者）
        viewModelScope.launch {
            ApiClient.wsEvents.collect { event ->
                when (event.type) {
                    "message" -> {
                        val msg = event.data as? Message ?: return@collect
                        onIncomingMessage(msg)
                    }
                    "conversation" -> loadConversations()
                }
            }
        }
    }

    /**
     * 处理 WebSocket 推送的新消息
     */
    private fun onIncomingMessage(msg: Message) {
        // 只有当前打开的会话才追加到消息列表
        if (msg.conversationId == _currentConvId.value) {
            addMessageDedup(msg)
        }
        // 刷新会话列表（更新最后一条消息和未读数）
        loadConversations()
    }

    /**
     * 消息去重：按 id 检查，已存在则不添加。
     * 这是发送消息（HTTP 响应）和接收推送（WS）共用的唯一入口。
     */
    private fun addMessageDedup(msg: Message) {
        _messages.update { existing ->
            if (existing.any { it.id == msg.id }) {
                existing
            } else {
                (existing + msg).sortedBy { it.id }
            }
        }
    }

    // ============================================================
    // 认证
    // ============================================================
    fun login(username: String, password: String) {
        viewModelScope.launch {
            _isLoading.value = true
            try {
                val resp = ApiClient.api.login(AuthRequest(username, password))
                if (resp.ok && resp.data != null) {
                    TokenStore.saveToken(getApplication(), resp.data.token)
                    ApiClient.setToken(resp.data.token)
                    _currentUser.value = resp.data.user
                    _isLoggedIn.value = true
                    ApiClient.connectWebSocket()
                    loadConversations()
                } else {
                    _error.tryEmit(resp.error ?: "登录失败")
                }
            } catch (e: Exception) {
                _error.tryEmit(ApiClient.parseError(e))
            }
            _isLoading.value = false
        }
    }

    fun register(username: String, password: String, displayName: String) {
        viewModelScope.launch {
            _isLoading.value = true
            try {
                val resp = ApiClient.api.register(
                    AuthRequest(username, password, displayName.ifBlank { username })
                )
                if (resp.ok && resp.data != null) {
                    TokenStore.saveToken(getApplication(), resp.data.token)
                    ApiClient.setToken(resp.data.token)
                    _currentUser.value = resp.data.user
                    _isLoggedIn.value = true
                    ApiClient.connectWebSocket()
                    loadConversations()
                } else {
                    _error.tryEmit(resp.error ?: "注册失败")
                }
            } catch (e: Exception) {
                _error.tryEmit(ApiClient.parseError(e))
            }
            _isLoading.value = false
        }
    }

    fun logout() {
        viewModelScope.launch {
            TokenStore.clearToken(getApplication())
            ApiClient.clearToken()
            ApiClient.disconnectWebSocket()
            _isLoggedIn.value = false
            _currentUser.value = null
            _conversations.value = emptyList()
            _messages.value = emptyList()
            _pendingMsgs.value = emptyList()
            _currentConvId.value = 0
            _currentConvName.value = ""
        }
    }

    // ============================================================
    // 会话
    // ============================================================
    fun loadConversations() {
        viewModelScope.launch {
            try {
                val resp = ApiClient.api.getConversations()
                if (resp.ok && resp.data != null) {
                    _conversations.value = resp.data
                }
            } catch (e: Exception) { /* 静默失败，避免列表页频繁弹错误 */ }
        }
    }

    /** 创建单聊，回调返回会话 ID（服务端幂等，已存在则返回已有会话） */
    fun createDirectConversation(userId: Long, onResult: (Long?) -> Unit) {
        viewModelScope.launch {
            try {
                val resp = ApiClient.api.createConversation(
                    CreateConvRequest(type = "direct", userId = userId)
                )
                if (resp.ok && resp.data != null) {
                    loadConversations()
                    onResult(resp.data.id)
                } else {
                    _error.tryEmit(resp.error ?: "创建会话失败")
                    onResult(null)
                }
            } catch (e: Exception) {
                _error.tryEmit(ApiClient.parseError(e))
                onResult(null)
            }
        }
    }

    /** 创建群聊，回调返回会话 ID */
    fun createGroup(name: String, userIds: List<Long>, onResult: (Long?) -> Unit) {
        viewModelScope.launch {
            try {
                val resp = ApiClient.api.createConversation(
                    CreateConvRequest(type = "group", name = name, userIds = userIds)
                )
                if (resp.ok && resp.data != null) {
                    loadConversations()
                    onResult(resp.data.id)
                } else {
                    _error.tryEmit(resp.error ?: "创建群聊失败")
                    onResult(null)
                }
            } catch (e: Exception) {
                _error.tryEmit(ApiClient.parseError(e))
                onResult(null)
            }
        }
    }

    // ============================================================
    // 消息
    // ============================================================

    /** 打开会话：设置当前会话上下文 + 加载历史消息 + 标记已读 */
    fun openConversation(convId: Long, name: String) {
        _currentConvId.value = convId
        _currentConvName.value = name
        _messages.value = emptyList()
        _pendingMsgs.value = emptyList() // 换会话时丢弃上一会话的未发送消息
        viewModelScope.launch {
            try {
                val resp = ApiClient.api.getMessages(convId)
                if (resp.ok && resp.data != null) {
                    _messages.value = resp.data
                }
                // 标记已读并刷新会话列表的未读数
                ApiClient.api.markRead(convId)
                loadConversations()
            } catch (e: Exception) {
                _error.tryEmit(ApiClient.parseError(e))
            }
        }
    }

    /** 关闭会话（返回列表页时调用），停止接收消息追加 */
    fun closeConversation() {
        _currentConvId.value = 0
        _currentConvName.value = ""
    }

    /**
     * APP 回到前台时调用：
     * 1. 重新连接 WebSocket（后台可能被系统断开）
     * 2. 刷新当前会话的消息（补上后台期间漏收的）
     * 3. 刷新会话列表（更新未读数）
     */
    fun onAppResume() {
        // 重新连接 WebSocket
        if (_isLoggedIn.value && ApiClient.getToken() != null) {
            ApiClient.connectWebSocket()
        }
        // 刷新会话列表
        loadConversations()
        // 刷新当前打开的会话消息
        val convId = _currentConvId.value
        if (convId > 0) {
            viewModelScope.launch {
                try {
                    val resp = ApiClient.api.getMessages(convId)
                    if (resp.ok && resp.data != null) {
                        // 用服务端全量数据替换本地列表，不追加（避免重复）
                        _messages.value = resp.data
                    }
                    ApiClient.api.markRead(convId)
                    loadConversations()
                } catch (e: Exception) { }
            }
        }
    }

    /** 发送文字消息（乐观 UI）：立即插入「发送中」气泡，成功后由服务端消息顶替，失败可重试 */
    fun sendTextMessage(content: String) {
        val convId = _currentConvId.value
        if (convId == 0L || content.isBlank()) return
        val pending = PendingMsg(tempMsgId--, content, System.currentTimeMillis(), PendingStatus.SENDING)
        _pendingMsgs.update { it + pending }
        dispatchText(pending)
    }

    /** 重试失败的待发送消息 */
    fun retryPendingMsg(tempId: Long) {
        val target = _pendingMsgs.value.find { it.tempId == tempId } ?: return
        if (target.status != PendingStatus.FAILED) return
        if (!isNetworkAvailable()) {
            _error.tryEmit("无网络连接，请联网后重试")
            return
        }
        _pendingMsgs.update { list ->
            list.map { if (it.tempId == tempId) it.copy(status = PendingStatus.SENDING) else it }
        }
        dispatchText(target)
    }

    /** 实际发送逻辑，成功移除待发送项，失败标记可重试 */
    private fun dispatchText(pending: PendingMsg) {
        val convId = _currentConvId.value
        if (convId == 0L) return
        // 断网快速失败：不发起请求，直接标记失败，避免长时间转圈
        if (!isNetworkAvailable()) {
            failPending(pending.tempId)
            _error.tryEmit("无网络连接，点击红色消息重试")
            return
        }
        viewModelScope.launch {
            try {
                val resp = ApiClient.api.sendMessage(
                    SendMessageRequest(conversationId = convId, content = pending.content)
                )
                if (resp.ok && resp.data != null) {
                    addMessageDedup(resp.data)
                    _pendingMsgs.update { list -> list.filterNot { it.tempId == pending.tempId } }
                } else {
                    failPending(pending.tempId)
                    _error.tryEmit("发送失败，点击红色消息可重试")
                }
            } catch (e: Exception) {
                failPending(pending.tempId)
                _error.tryEmit("发送失败，点击红色消息可重试")
            }
        }
    }

    /** 检查网络是否可用（Wi-Fi/蜂窝/以太网均可） */
    private fun isNetworkAvailable(): Boolean {
        val cm = getApplication<Application>().getSystemService(Context.CONNECTIVITY_SERVICE) as? ConnectivityManager
            ?: return true // 拿不到服务时不拦截（降级放行，让 OkHttp 超时兜底）
        val network = cm.activeNetwork ?: return false
        val caps = cm.getNetworkCapabilities(network) ?: return false
        return caps.hasCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)
    }

    private fun failPending(tempId: Long) {
        _pendingMsgs.update { list ->
            list.map { if (it.tempId == tempId) it.copy(status = PendingStatus.FAILED) else it }
        }
    }

    /** 发送文件消息（图片/文件），先上传再发送 */
    fun sendFileMessage(fileId: Long, type: String) {
        val convId = _currentConvId.value
        if (convId == 0L) return
        viewModelScope.launch {
            try {
                val resp = ApiClient.api.sendMessage(
                    SendMessageRequest(conversationId = convId, fileId = fileId, type = type)
                )
                if (resp.ok && resp.data != null) {
                    addMessageDedup(resp.data)
                } else {
                    _error.tryEmit(resp.error ?: "发送失败")
                }
            } catch (e: Exception) {
                _error.tryEmit(ApiClient.parseError(e))
            }
        }
    }

    /** 上传文件，回调返回文件信息 */
    fun uploadFile(bytes: ByteArray, fileName: String, mime: String, onResult: (FileInfo?) -> Unit) {
        viewModelScope.launch {
            try {
                val resp = ApiClient.uploadFile(bytes, fileName, mime)
                if (resp.ok && resp.data != null) {
                    onResult(resp.data)
                } else {
                    _error.tryEmit(resp.error ?: "上传失败")
                    onResult(null)
                }
            } catch (e: Exception) {
                _error.tryEmit(ApiClient.parseError(e))
                onResult(null)
            }
        }
    }

    // ============================================================
    // 头像
    // ============================================================

    /** 上传头像，成功后更新 currentUser */
    fun uploadAvatar(bytes: ByteArray, fileName: String, mime: String, onResult: (Boolean) -> Unit) {
        viewModelScope.launch {
            try {
                val resp = ApiClient.uploadAvatar(bytes, fileName, mime)
                if (resp.ok && resp.data != null) {
                    _currentUser.value = resp.data
                    onResult(true)
                } else {
                    _error.tryEmit(resp.error ?: "头像上传失败")
                    onResult(false)
                }
            } catch (e: Exception) {
                _error.tryEmit(ApiClient.parseError(e))
                onResult(false)
            }
        }
    }

    // ============================================================
    // 群聊邀请成员（通讯录多选）
    // ============================================================

    /** 加载通讯录：与我共处过任意会话的所有用户 */
    fun loadContacts(onResult: (List<User>) -> Unit) {
        viewModelScope.launch {
            try {
                val resp = ApiClient.api.getContacts()
                if (resp.ok && resp.data != null) {
                    onResult(resp.data)
                } else {
                    onResult(emptyList())
                }
            } catch (e: Exception) {
                onResult(emptyList())
            }
        }
    }

    /** 加载会话详情（含完整成员列表，用于排除已在群里的成员） */
    fun loadConversation(convId: Long, onResult: (Conversation?) -> Unit) {
        viewModelScope.launch {
            try {
                val resp = ApiClient.api.getConversation(convId)
                if (resp.ok && resp.data != null) {
                    onResult(resp.data)
                } else {
                    onResult(null)
                }
            } catch (e: Exception) {
                onResult(null)
            }
        }
    }

    /** 群聊批量拉人 */
    fun inviteMembers(convId: Long, userIds: List<Long>, onResult: (Boolean) -> Unit) {
        viewModelScope.launch {
            try {
                val resp = ApiClient.api.addMembers(convId, com.schat.app.data.AddMembersRequest(userIds))
                if (resp.ok) {
                    loadConversations()
                    onResult(true)
                } else {
                    _error.tryEmit(resp.error ?: "邀请失败")
                    onResult(false)
                }
            } catch (e: Exception) {
                _error.tryEmit(ApiClient.parseError(e))
                onResult(false)
            }
        }
    }
}
