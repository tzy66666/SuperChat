package com.schat.app.ui.screens

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.PersonAdd
import androidx.compose.material.icons.filled.Search
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.schat.app.data.User
import com.schat.app.ui.AppViewModel
import com.schat.app.ui.theme.*
import kotlinx.coroutines.launch

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun NewChatScreen(
    vm: AppViewModel,
    onBack: () -> Unit,
    onConversationCreated: (Long, String) -> Unit
) {
    var query by remember { mutableStateOf("") }
    var users by remember { mutableStateOf<List<User>>(emptyList()) }
    var showGroupDialog by remember { mutableStateOf(false) }
    var isCreating by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()

    // 防抖搜索
    LaunchedEffect(query) {
        if (query.isNotBlank()) {
            kotlinx.coroutines.delay(300)
            if (query.isNotBlank()) {
                scope.launch {
                    try {
                        val resp = com.schat.app.network.ApiClient.api.searchUsers(query.trim())
                        if (resp.ok && resp.data != null) {
                            users = resp.data
                        }
                    } catch (e: Exception) { }
                }
            }
        } else {
            users = emptyList()
        }
    }

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text("发起聊天", fontWeight = FontWeight.SemiBold) },
                navigationIcon = {
                    IconButton(onClick = onBack) {
                        Icon(Icons.AutoMirrored.Filled.ArrowBack, "返回")
                    }
                },
                actions = {
                    // 从通讯录选人建群
                    IconButton(onClick = { if (!isCreating) showGroupDialog = true }) {
                        Icon(Icons.Filled.PersonAdd, "创建群聊")
                    }
                },
                colors = TopAppBarDefaults.topAppBarColors(
                    containerColor = SurfaceWhite,
                    titleContentColor = TextPrimary
                )
            )
        }
    ) { padding ->
        Column(modifier = Modifier.padding(padding)) {
            OutlinedTextField(
                value = query,
                onValueChange = { query = it },
                placeholder = { Text("搜索用户名...") },
                leadingIcon = { Icon(Icons.Filled.Search, null) },
                modifier = Modifier.fillMaxWidth().padding(16.dp),
                singleLine = true,
                shape = RoundedCornerShape(24.dp)
            )

            if (query.isNotBlank() && users.isEmpty()) {
                Box(
                    modifier = Modifier.fillMaxSize(),
                    contentAlignment = Alignment.Center
                ) {
                    Text("未找到用户", color = TextSecondary, fontSize = 14.sp)
                }
            } else {
                LazyColumn {
                    items(users, key = { it.id }) { user ->
                        Row(
                            modifier = Modifier
                                .fillMaxWidth()
                                .clickable(enabled = !isCreating) {
                                    isCreating = true
                                    vm.createDirectConversation(user.id) { convId ->
                                        isCreating = false
                                        if (convId != null) {
                                            onConversationCreated(convId, user.displayName)
                                        }
                                    }
                                }
                                .padding(horizontal = 16.dp, vertical = 12.dp),
                            verticalAlignment = Alignment.CenterVertically
                        ) {
                            Box(
                                modifier = Modifier
                                    .size(48.dp)
                                    .clip(CircleShape)
                                    .background(BluePrimary),
                                contentAlignment = Alignment.Center
                            ) {
                                Text(
                                    text = user.displayName.take(1),
                                    color = TextWhite,
                                    fontWeight = FontWeight.Bold,
                                    fontSize = 18.sp
                                )
                            }
                            Spacer(modifier = Modifier.width(12.dp))
                            Column {
                                Text(
                                    text = user.displayName,
                                    fontWeight = FontWeight.SemiBold,
                                    fontSize = 15.sp
                                )
                                Text(
                                    text = "@${user.username}",
                                    color = TextSecondary,
                                    fontSize = 13.sp
                                )
                            }
                        }
                        HorizontalDivider(color = DividerGray, thickness = 0.5.dp)
                    }
                }
            }
        }
    }

    // 建群对话框（从通讯录多选，不再依赖全站搜索）
    if (showGroupDialog) {
        var contacts by remember { mutableStateOf<List<User>?>(null) } // null = 加载中
        var searchQuery by remember { mutableStateOf("") }
        var selectedIds by remember { mutableStateOf<Set<Long>>(emptySet()) }
        var groupName by remember { mutableStateOf("") }

        // 打开时加载通讯录
        LaunchedEffect(Unit) {
            vm.loadContacts { list -> contacts = list }
        }

        // 本地搜索过滤（只过滤联系人）
        val filtered = remember(contacts, searchQuery) {
            val all = contacts ?: emptyList()
            val q = searchQuery.trim()
            if (q.isEmpty()) all
            else all.filter {
                it.username.contains(q, true) || it.displayName.contains(q, true)
            }
        }

        AlertDialog(
            onDismissRequest = { if (!isCreating) showGroupDialog = false },
            title = { Text("创建群聊", fontWeight = FontWeight.SemiBold) },
            text = {
                Column {
                    OutlinedTextField(
                        value = groupName,
                        onValueChange = { groupName = it },
                        label = { Text("群名称") },
                        singleLine = true,
                        modifier = Modifier.fillMaxWidth()
                    )
                    Spacer(modifier = Modifier.height(8.dp))
                    OutlinedTextField(
                        value = searchQuery,
                        onValueChange = { searchQuery = it },
                        placeholder = { Text("搜索联系人...", fontSize = 14.sp) },
                        singleLine = true,
                        shape = RoundedCornerShape(12.dp),
                        modifier = Modifier.fillMaxWidth()
                    )
                    Spacer(modifier = Modifier.height(8.dp))
                    Text("已选 ${selectedIds.size} 人", fontSize = 12.sp, color = TextSecondary)
                    Spacer(modifier = Modifier.height(4.dp))

                    when {
                        contacts == null -> {
                            // 加载中
                            Box(
                                modifier = Modifier.fillMaxWidth().height(180.dp),
                                contentAlignment = Alignment.Center
                            ) {
                                CircularProgressIndicator()
                            }
                        }
                        filtered.isEmpty() -> {
                            Box(
                                modifier = Modifier.fillMaxWidth().height(180.dp),
                                contentAlignment = Alignment.Center
                            ) {
                                Text(
                                    text = if (contacts.isNullOrEmpty())
                                        "暂无联系人\n先和人发起聊天后，就能拉他建群了"
                                    else "未找到匹配的联系人",
                                    fontSize = 13.sp,
                                    color = TextSecondary,
                                    textAlign = TextAlign.Center
                                )
                            }
                        }
                        else -> {
                            LazyColumn(modifier = Modifier.heightIn(max = 240.dp)) {
                                items(filtered, key = { it.id }) { user ->
                                    val isSelected = user.id in selectedIds
                                    Row(
                                        modifier = Modifier
                                            .fillMaxWidth()
                                            .clickable {
                                                selectedIds = if (isSelected) selectedIds - user.id
                                                else selectedIds + user.id
                                            }
                                            .padding(vertical = 6.dp),
                                        verticalAlignment = Alignment.CenterVertically
                                    ) {
                                        Checkbox(
                                            checked = isSelected,
                                            onCheckedChange = null
                                        )
                                        Spacer(modifier = Modifier.width(8.dp))
                                        Avatar(
                                            name = user.displayName.ifBlank { user.username },
                                            avatarUrl = user.avatarUrl,
                                            size = 36
                                        )
                                        Spacer(modifier = Modifier.width(10.dp))
                                        Column {
                                            Text(
                                                text = user.displayName.ifBlank { user.username },
                                                fontSize = 14.sp,
                                                fontWeight = FontWeight.Medium
                                            )
                                            Text(
                                                text = "@${user.username}",
                                                fontSize = 12.sp,
                                                color = TextSecondary
                                            )
                                        }
                                    }
                                }
                            }
                        }
                    }
                }
            },
            confirmButton = {
                TextButton(
                    onClick = {
                        if (groupName.isNotBlank() && selectedIds.isNotEmpty() && !isCreating) {
                            isCreating = true
                            val name = groupName.trim()
                            val ids = selectedIds.toList()
                            vm.createGroup(name, ids) { convId ->
                                isCreating = false
                                if (convId != null) {
                                    showGroupDialog = false
                                    onConversationCreated(convId, name)
                                }
                            }
                        }
                    },
                    enabled = groupName.isNotBlank() && selectedIds.isNotEmpty() && !isCreating
                ) {
                    if (isCreating) {
                        CircularProgressIndicator(strokeWidth = 2.dp, modifier = Modifier.size(18.dp))
                    } else {
                        Text("创建")
                    }
                }
            },
            dismissButton = {
                TextButton(
                    onClick = { if (!isCreating) showGroupDialog = false },
                    enabled = !isCreating
                ) { Text("取消") }
            }
        )
    }
}
