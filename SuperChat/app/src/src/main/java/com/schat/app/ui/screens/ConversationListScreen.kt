package com.schat.app.ui.screens

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.Logout
import androidx.compose.material.icons.filled.ChatBubble
import androidx.compose.material.icons.filled.Edit
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.schat.app.data.Conversation
import com.schat.app.ui.AppViewModel
import com.schat.app.ui.theme.*

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ConversationListScreen(
    vm: AppViewModel,
    onOpenConversation: (Long, String) -> Unit,
    onNewChat: () -> Unit,
    onProfile: () -> Unit
) {
    val conversations by vm.conversations.collectAsState()
    val currentUser by vm.currentUser.collectAsState()

    // 页面可见时刷新一次
    LaunchedEffect(Unit) { vm.loadConversations() }

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text("SuperChat", fontWeight = FontWeight.Bold) },
                colors = TopAppBarDefaults.topAppBarColors(
                    containerColor = BluePrimary,
                    titleContentColor = TextWhite
                ),
                actions = {
                    // 点击自己的头像进入个人资料
                    IconButton(onClick = onProfile) {
                        Avatar(
                            name = currentUser?.displayName ?: "U",
                            avatarUrl = currentUser?.avatarUrl ?: "",
                            size = 36
                        )
                    }
                    IconButton(onClick = { vm.logout() }) {
                        Icon(Icons.AutoMirrored.Filled.Logout, "退出", tint = TextWhite)
                    }
                }
            )
        },
        floatingActionButton = {
            FloatingActionButton(
                onClick = onNewChat,
                containerColor = BluePrimary,
                contentColor = TextWhite
            ) {
                Icon(Icons.Filled.Edit, "新聊天")
            }
        }
    ) { padding ->
        val list = conversations
        if (list.isEmpty()) {
            Box(
                modifier = Modifier.fillMaxSize().padding(padding),
                contentAlignment = Alignment.Center
            ) {
                Column(horizontalAlignment = Alignment.CenterHorizontally) {
                    Icon(
                        Icons.Filled.ChatBubble, null,
                        tint = DividerGray, modifier = Modifier.size(64.dp)
                    )
                    Spacer(modifier = Modifier.height(12.dp))
                    Text("还没有会话，点击右下角开始", color = TextSecondary, fontSize = 14.sp)
                }
            }
        } else {
            LazyColumn(modifier = Modifier.padding(padding)) {
                items(list, key = { it.id }) { conv ->
                    val uid = currentUser?.id ?: 0
                    val displayName = convDisplayName(conv, uid)
                    // 单聊取对方头像，群聊用空（显示首字母）
                    val avatarUrl = if (conv.type == "direct" && !conv.members.isNullOrEmpty()) {
                        conv.members.firstOrNull { it.userId != uid }?.avatarUrl ?: ""
                    } else ""
                    ConversationItem(
                        displayName = displayName,
                        avatarUrl = avatarUrl,
                        conv = conv,
                        onClick = { onOpenConversation(conv.id, displayName) }
                    )
                    HorizontalDivider(color = DividerGray, thickness = 0.5.dp)
                }
            }
        }
    }
}

/** 单聊显示对方名字，群聊显示群名 */
fun convDisplayName(conv: Conversation, currentUserId: Long): String {
    return if (conv.type == "direct" && !conv.members.isNullOrEmpty()) {
        conv.members.firstOrNull { it.userId != currentUserId }?.displayName
            ?: conv.name.ifEmpty { "聊天" }
    } else {
        conv.name.ifEmpty { "群聊" }
    }
}

@Composable
private fun ConversationItem(
    displayName: String,
    avatarUrl: String,
    conv: Conversation,
    onClick: () -> Unit
) {
    val lastMsg = conv.lastMessage
    val lastMsgText = when {
        lastMsg == null -> ""
        lastMsg.type == "text" -> lastMsg.content
        lastMsg.type == "image" -> "[图片]"
        lastMsg.type == "file" -> "[文件] ${lastMsg.file?.name ?: ""}"
        else -> ""
    }

    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clickable(onClick = onClick)
            .padding(horizontal = 16.dp, vertical = 12.dp),
        verticalAlignment = Alignment.CenterVertically
    ) {
        // 头像
        Avatar(
            name = displayName,
            avatarUrl = avatarUrl,
            isGroup = conv.type == "group",
            size = 52
        )

        Spacer(modifier = Modifier.width(12.dp))

        Column(modifier = Modifier.weight(1f)) {
            Text(
                text = displayName,
                fontWeight = FontWeight.SemiBold, fontSize = 16.sp,
                maxLines = 1, overflow = TextOverflow.Ellipsis
            )
            if (lastMsgText.isNotEmpty()) {
                Text(
                    text = lastMsgText,
                    color = TextSecondary, fontSize = 13.sp,
                    maxLines = 1, overflow = TextOverflow.Ellipsis
                )
            }
        }

        if (conv.unread > 0) {
            Box(
                modifier = Modifier
                    .size(22.dp)
                    .clip(CircleShape)
                    .background(UnreadRed),
                contentAlignment = Alignment.Center
            ) {
                Text(
                    text = if (conv.unread > 99) "99+" else conv.unread.toString(),
                    color = TextWhite, fontSize = 11.sp, fontWeight = FontWeight.Bold
                )
            }
        }
    }
}
