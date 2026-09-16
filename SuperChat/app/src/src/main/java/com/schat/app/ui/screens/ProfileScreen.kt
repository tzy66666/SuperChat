package com.schat.app.ui.screens

import android.net.Uri
import android.provider.OpenableColumns
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.schat.app.ui.AppViewModel
import com.schat.app.ui.theme.*
import kotlinx.coroutines.launch

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ProfileScreen(
    vm: AppViewModel,
    onBack: () -> Unit
) {
    val currentUser by vm.currentUser.collectAsState()
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var isUploading by remember { mutableStateOf(false) }

    val imagePicker = rememberLauncherForActivityResult(
        ActivityResultContracts.GetContent()
    ) { uri: Uri? ->
        if (uri != null) {
            isUploading = true
            scope.launch {
                try {
                    val resolver = context.contentResolver
                    val mime = resolver.getType(uri) ?: "image/jpeg"
                    val fileName = resolver.query(uri, null, null, null, null)?.use { c ->
                        c.moveToFirst()
                        val idx = c.getColumnIndex(OpenableColumns.DISPLAY_NAME)
                        if (idx >= 0) c.getString(idx) else "avatar.jpg"
                    } ?: "avatar.jpg"
                    val bytes = resolver.openInputStream(uri)?.use { it.readBytes() }
                    if (bytes != null) {
                        vm.uploadAvatar(bytes, fileName, mime) { success ->
                            isUploading = false
                        }
                    } else {
                        isUploading = false
                    }
                } catch (e: Exception) {
                    isUploading = false
                }
            }
        }
    }

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text("个人资料", fontWeight = FontWeight.SemiBold) },
                navigationIcon = {
                    IconButton(onClick = onBack) {
                        Icon(Icons.AutoMirrored.Filled.ArrowBack, "返回")
                    }
                },
                colors = TopAppBarDefaults.topAppBarColors(
                    containerColor = SurfaceWhite,
                    titleContentColor = TextPrimary
                )
            )
        }
    ) { padding ->
        Column(
            modifier = Modifier
                .fillMaxSize()
                .padding(padding)
                .padding(24.dp),
            horizontalAlignment = Alignment.CenterHorizontally
        ) {
            Spacer(modifier = Modifier.height(40.dp))

            // 头像（可点击上传）
            Box(
                modifier = Modifier
                    .size(120.dp)
                    .clip(CircleShape),
                contentAlignment = Alignment.Center
            ) {
                Avatar(
                    name = currentUser?.displayName ?: "U",
                    avatarUrl = currentUser?.avatarUrl ?: "",
                    size = 120
                )
            }

            Spacer(modifier = Modifier.height(16.dp))

            if (isUploading) {
                CircularProgressIndicator(strokeWidth = 2.dp, modifier = Modifier.size(24.dp))
            } else {
                TextButton(onClick = { imagePicker.launch("image/*") }) {
                    Text("更换头像", color = BluePrimary, fontSize = 16.sp)
                }
            }

            Spacer(modifier = Modifier.height(32.dp))

            // 用户信息
            Card(
                modifier = Modifier.fillMaxWidth(),
                shape = androidx.compose.foundation.shape.RoundedCornerShape(16.dp)
            ) {
                Column(modifier = Modifier.padding(20.dp)) {
                    InfoRow("用户名", currentUser?.username ?: "")
                    HorizontalDivider(modifier = Modifier.padding(vertical = 12.dp))
                    InfoRow("昵称", currentUser?.displayName ?: "")
                    HorizontalDivider(modifier = Modifier.padding(vertical = 12.dp))
                    InfoRow("用户ID", currentUser?.id?.toString() ?: "")
                }
            }
        }
    }
}

@Composable
private fun InfoRow(label: String, value: String) {
    Row(
        modifier = Modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.SpaceBetween
    ) {
        Text(label, color = TextSecondary, fontSize = 14.sp)
        Text(value, fontWeight = FontWeight.Medium, fontSize = 14.sp)
    }
}
