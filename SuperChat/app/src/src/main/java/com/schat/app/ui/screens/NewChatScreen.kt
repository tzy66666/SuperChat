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
import androidx.compose.material.icons.filled.Group
import androidx.compose.material.icons.filled.Search
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.font.FontWeight
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
    var selectedUsers by remember { mutableStateOf<Set<Long>>(emptySet()) }
    var groupName by remember { mutableStateOf("") }
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
                    IconButton(
                        onClick = {
                            if (users.isEmpty()) {
                                showGroupDialog = true // 打开对话框，里面会提示去搜索
                            } else if (!isCreating) {
                                showGroupDialog = true
                            }
                        }
                    ) {
                        Icon(Icons.Filled.Group, "创建群聊")
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

    // 建群对话框
    if (showGroupDialog) {
        AlertDialog(
            onDismissRequest = { if (!isCreating) showGroupDialog = false },
            title = { Text("创建群聊") },
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
                    Text("已选 ${selectedUsers.size} 人", fontSize = 14.sp, color = TextSecondary)
                    Spacer(modifier = Modifier.height(4.dp))
                    if (users.isEmpty()) {
                        // 没搜过用户，提示去搜索
                        Box(
                            modifier = Modifier.height(200.dp).fillMaxWidth(),
                            contentAlignment = Alignment.Center
                        ) {
                            Text("请先搜索用户，再选择群成员", color = TextSecondary, fontSize = 14.sp)
                        }
                    } else {
                        LazyColumn(modifier = Modifier.height(200.dp)) {
                        items(users, key = { it.id }) { user ->
                            Row(
                                modifier = Modifier
                                    .fillMaxWidth()
                                    .clickable {
                                        selectedUsers = if (user.id in selectedUsers)
                                            selectedUsers - user.id
                                        else selectedUsers + user.id
                                    }
                                    .padding(vertical = 8.dp),
                                verticalAlignment = Alignment.CenterVertically
                            ) {
                                Checkbox(
                                    checked = user.id in selectedUsers,
                                    onCheckedChange = {
                                        selectedUsers = if (it) selectedUsers + user.id
                                        else selectedUsers - user.id
                                    }
                                )
                                Text(user.displayName, fontSize = 14.sp)
                                Text(" @${user.username}", fontSize = 12.sp, color = TextSecondary)
                            }
                        }
                    }
                    }
                }
            },
            confirmButton = {
                TextButton(
                    onClick = {
                        if (groupName.isNotBlank() && selectedUsers.isNotEmpty() && !isCreating) {
                            isCreating = true
                            val name = groupName.trim()
                            val ids = selectedUsers.toList()
                            vm.createGroup(name, ids) { convId ->
                                isCreating = false
                                if (convId != null) {
                                    showGroupDialog = false
                                    onConversationCreated(convId, name)
                                }
                            }
                        }
                    },
                    enabled = groupName.isNotBlank() && selectedUsers.isNotEmpty() && !isCreating
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
