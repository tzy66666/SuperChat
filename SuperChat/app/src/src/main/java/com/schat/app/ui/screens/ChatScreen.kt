package com.schat.app.ui.screens

import android.content.ContentValues
import android.content.Intent
import android.net.Uri
import android.os.Build
import android.os.Environment
import android.provider.OpenableColumns
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.automirrored.filled.Send
import androidx.compose.material.icons.filled.AttachFile
import androidx.compose.material.icons.filled.InsertDriveFile
import androidx.compose.material.icons.filled.OpenInNew
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import coil.compose.AsyncImage
import com.schat.app.data.Message
import com.schat.app.network.ApiClient
import com.schat.app.ui.AppViewModel
import com.schat.app.ui.theme.*
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import okhttp3.OkHttpClient
import okhttp3.Request
import java.io.File
import java.io.FileOutputStream
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ChatScreen(
    vm: AppViewModel,
    convId: Long,
    convName: String,
    onBack: () -> Unit
) {
    val messages by vm.messages.collectAsState()
    val currentUser by vm.currentUser.collectAsState()
    val context = LocalContext.current

    var inputText by remember { mutableStateOf("") }
    var isUploading by remember { mutableStateOf(false) }
    val listState = rememberLazyListState()
    val scope = rememberCoroutineScope()

    // 每条文件消息的下载状态：messageId -> DownloadState
    val downloadStates = remember { mutableStateMapOf<Long, DownloadState>() }

    LaunchedEffect(messages.size) {
        if (messages.isNotEmpty()) {
            listState.animateScrollToItem(messages.lastIndex)
        }
    }

    val filePicker = rememberLauncherForActivityResult(
        ActivityResultContracts.GetContent()
    ) { uri: Uri? ->
        if (uri != null) {
            isUploading = true
            scope.launch {
                try {
                    val resolver = context.contentResolver
                    val mime = resolver.getType(uri) ?: "application/octet-stream"
                    val fileName = resolver.query(uri, null, null, null, null)?.use { c ->
                        c.moveToFirst()
                        val idx = c.getColumnIndex(OpenableColumns.DISPLAY_NAME)
                        if (idx >= 0) c.getString(idx) else "file"
                    } ?: "file"
                    val bytes = resolver.openInputStream(uri)?.use { it.readBytes() }
                    if (bytes != null) {
                        vm.uploadFile(bytes, fileName, mime) { fileInfo ->
                            isUploading = false
                            if (fileInfo != null) {
                                val type = if (mime.startsWith("image/")) "image" else "file"
                                vm.sendFileMessage(fileInfo.id, type)
                            }
                        }
                    } else { isUploading = false }
                } catch (e: Exception) { isUploading = false }
            }
        }
    }

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text(convName, fontWeight = FontWeight.SemiBold, maxLines = 1) },
                navigationIcon = {
                    IconButton(onClick = onBack) {
                        Icon(Icons.AutoMirrored.Filled.ArrowBack, "返回")
                    }
                },
                colors = TopAppBarDefaults.topAppBarColors(
                    containerColor = SurfaceWhite,
                    titleContentColor = TextPrimary
                ),
                actions = {
                    if (isUploading) {
                        CircularProgressIndicator(
                            strokeWidth = 2.dp,
                            modifier = Modifier.size(24.dp).padding(end = 16.dp)
                        )
                    }
                }
            )
        },
        bottomBar = {
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .background(SurfaceWhite)
                    .navigationBarsPadding()
                    .imePadding()
                    .padding(8.dp),
                verticalAlignment = Alignment.CenterVertically
            ) {
                IconButton(
                    onClick = { filePicker.launch("*/*") },
                    enabled = !isUploading
                ) {
                    Icon(Icons.Filled.AttachFile, "发送文件", tint = BluePrimary)
                }
                OutlinedTextField(
                    value = inputText,
                    onValueChange = { inputText = it },
                    placeholder = { Text("输入消息...", fontSize = 14.sp) },
                    modifier = Modifier.weight(1f),
                    shape = RoundedCornerShape(24.dp),
                    maxLines = 4
                )
                Spacer(modifier = Modifier.width(8.dp))
                FloatingActionButton(
                    onClick = {
                        if (inputText.isNotBlank()) {
                            vm.sendTextMessage(inputText.trim())
                            inputText = ""
                        }
                    },
                    containerColor = BluePrimary,
                    contentColor = TextWhite,
                    modifier = Modifier.size(44.dp)
                ) {
                    Icon(Icons.AutoMirrored.Filled.Send, "发送")
                }
            }
        }
    ) { padding ->
        LazyColumn(
            modifier = Modifier
                .fillMaxSize()
                .background(BgDark)
                .padding(padding)
                .padding(horizontal = 12.dp),
            state = listState
        ) {
            items(messages, key = { it.id }) { msg ->
                MessageBubble(
                    msg = msg,
                    isMe = msg.senderId == currentUser?.id,
                    context = context,
                    downloadStates = downloadStates,
                    onDownload = { message, targetFile ->
                        scope.launch {
                            downloadFile(message, targetFile, downloadStates, context)
                        }
                    },
                    onOpen = { file ->
                        openFile(context, file)
                    }
                )
            }
        }
    }
}

// ---- 下载状态 ----
sealed class DownloadState {
    object NotDownloaded : DownloadState()
    data class Downloading(val progress: Int) : DownloadState()  // 0-100
    data class Done(val file: File) : DownloadState()
    data class Failed(val msg: String) : DownloadState()
}

// ---- 下载逻辑 ----
private suspend fun downloadFile(
    msg: Message,
    targetDir: File,
    downloadStates: MutableMap<Long, DownloadState>,
    context: android.content.Context
) {
    val url = msg.file?.url ?: return
    val fileName = msg.file?.name ?: "file"
    val msgId = msg.id

    downloadStates[msgId] = DownloadState.Downloading(0)

    try {
        val fullUrl = ApiClient.fullFileUrl(url)
        val client = OkHttpClient()
        val request = Request.Builder().url(fullUrl).build()

        withContext(Dispatchers.IO) {
            val response = client.newCall(request).execute()
            if (!response.isSuccessful) {
                downloadStates[msgId] = DownloadState.Failed("HTTP ${response.code}")
                return@withContext
            }

            val totalBytes = response.body?.contentLength() ?: -1
            val downloadDir = File(
                Environment.getExternalStoragePublicDirectory(Environment.DIRECTORY_DOWNLOADS),
                "SuperChat"
            )
            if (!downloadDir.exists()) downloadDir.mkdirs()

            val file = File(downloadDir, fileName)
            var duplicateName = fileName
            var counter = 1
            while (File(downloadDir, duplicateName).exists()) {
                val dotIdx = fileName.lastIndexOf('.')
                duplicateName = if (dotIdx > 0) {
                    fileName.substring(0, dotIdx) + "($counter)" + fileName.substring(dotIdx)
                } else {
                    "$fileName($counter)"
                }
                counter++
            }
            val targetFile = File(downloadDir, duplicateName)

            FileOutputStream(targetFile).use { fos ->
                val body = response.body ?: throw Exception("空响应")
                val inputStream = body.byteStream()
                var totalRead = 0L
                val buffer = ByteArray(8192)
                while (true) {
                    val read = inputStream.read(buffer)
                    if (read == -1) break
                    fos.write(buffer, 0, read)
                    totalRead += read
                    if (totalBytes > 0) {
                        val progress = (totalRead * 100 / totalBytes).toInt()
                        downloadStates[msgId] = DownloadState.Downloading(progress)
                    }
                }
                fos.flush()
            }

            // 通知媒体扫描器，让文件在文件管理器中可见
            val intent = Intent(Intent.ACTION_MEDIA_SCANNER_SCAN_FILE).apply {
                data = Uri.fromFile(targetFile)
            }
            context.sendBroadcast(intent)

            downloadStates[msgId] = DownloadState.Done(targetFile)
        }
    } catch (e: Exception) {
        downloadStates[msgId] = DownloadState.Failed(e.message ?: "下载失败")
    }
}

// ---- 打开文件 ----
private fun openFile(context: android.content.Context, file: File) {
    val uri = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.N) {
        androidx.core.content.FileProvider.getUriForFile(
            context,
            "${context.packageName}.fileprovider",
            file
        )
    } else {
        Uri.fromFile(file)
    }
    val intent = Intent(Intent.ACTION_VIEW).apply {
        setDataAndType(uri, context.contentResolver.getType(uri) ?: "*/*")
        addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
        addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
    }
    try {
        context.startActivity(intent)
    } catch (e: Exception) {
        // 没有可打开的应用，用系统选择器
        val chooser = Intent.createChooser(intent, "打开文件").apply {
            addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        }
        context.startActivity(chooser)
    }
}

// ---- 气泡 ----
@Composable
private fun MessageBubble(
    msg: Message,
    isMe: Boolean,
    context: android.content.Context,
    downloadStates: MutableMap<Long, DownloadState>,
    onDownload: (Message, File) -> Unit,
    onOpen: (File) -> Unit
) {
    val alignment = if (isMe) Alignment.End else Alignment.Start
    val bgColor = if (isMe) BubbleMe else BubbleOther
    val textColor = if (isMe) TextWhite else TextPrimary
    val timeFmt = remember { SimpleDateFormat("HH:mm", Locale.getDefault()) }

    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(vertical = 4.dp),
        horizontalAlignment = alignment
    ) {
        Box(
            modifier = Modifier
                .clip(
                    RoundedCornerShape(
                        topStart = 16.dp, topEnd = 16.dp,
                        bottomStart = if (isMe) 16.dp else 4.dp,
                        bottomEnd = if (isMe) 4.dp else 16.dp
                    )
                )
                .background(bgColor)
                .padding(horizontal = 14.dp, vertical = 10.dp)
                .widthIn(max = 280.dp)
        ) {
            when (msg.type) {
                "text" -> Column {
                    Text(text = msg.content, color = textColor, fontSize = 15.sp)
                    Spacer(modifier = Modifier.height(4.dp))
                    Text(
                        text = timeFmt.format(Date(msg.createdAt)),
                        color = textColor.copy(alpha = 0.5f),
                        fontSize = 10.sp
                    )
                }
                "image" -> {
                    val url = msg.file?.url
                    if (url != null) {
                        Column {
                            AsyncImage(
                                model = ApiClient.fullFileUrl(url),
                                contentDescription = "图片",
                                modifier = Modifier
                                    .size(200.dp)
                                    .clip(RoundedCornerShape(12.dp))
                            )
                            Spacer(modifier = Modifier.height(4.dp))
                            Text(
                                text = timeFmt.format(Date(msg.createdAt)),
                                color = textColor.copy(alpha = 0.5f),
                                fontSize = 10.sp
                            )
                        }
                    }
                }
                "file" -> {
                    val state = downloadStates[msg.id] ?: DownloadState.NotDownloaded
                    val fileName = msg.file?.name ?: "文件"
                    val fileSize = formatFileSize(msg.file?.size ?: 0)

                    Column {
                        Row(verticalAlignment = Alignment.CenterVertically) {
                            Icon(
                                Icons.Filled.InsertDriveFile, null,
                                tint = textColor, modifier = Modifier.size(20.dp)
                            )
                            Spacer(modifier = Modifier.width(6.dp))
                            Text(
                                text = fileName,
                                color = textColor, fontSize = 14.sp,
                                maxLines = 1
                            )
                        }
                        Spacer(modifier = Modifier.height(6.dp))

                        when (state) {
                            is DownloadState.NotDownloaded -> {
                                // 下载按钮
                                Row(
                                    modifier = Modifier.clickable {
                                        onDownload(msg, File(""))
                                    },
                                    verticalAlignment = Alignment.CenterVertically
                                ) {
                                    Text(
                                        text = "$fileSize · 点击下载",
                                        color = textColor.copy(alpha = 0.7f),
                                        fontSize = 12.sp
                                    )
                                }
                            }
                            is DownloadState.Downloading -> {
                                // 进度条
                                Column {
                                    LinearProgressIndicator(
                                        progress = { state.progress / 100f },
                                        modifier = Modifier.fillMaxWidth().height(4.dp),
                                        color = if (isMe) TextWhite else BluePrimary,
                                        trackColor = textColor.copy(alpha = 0.2f)
                                    )
                                    Spacer(modifier = Modifier.height(2.dp))
                                    Text(
                                        text = "下载中 ${state.progress}%",
                                        color = textColor.copy(alpha = 0.7f),
                                        fontSize = 11.sp
                                    )
                                }
                            }
                            is DownloadState.Done -> {
                                // 下载完成，点击打开
                                Row(
                                    modifier = Modifier.clickable { onOpen(state.file) },
                                    verticalAlignment = Alignment.CenterVertically
                                ) {
                                    Icon(
                                        Icons.Filled.OpenInNew, null,
                                        tint = textColor.copy(alpha = 0.8f),
                                        modifier = Modifier.size(14.dp)
                                    )
                                    Spacer(modifier = Modifier.width(4.dp))
                                    Text(
                                        text = "$fileSize · 已下载，点击打开",
                                        color = textColor.copy(alpha = 0.8f),
                                        fontSize = 12.sp
                                    )
                                }
                            }
                            is DownloadState.Failed -> {
                                Row(
                                    modifier = Modifier.clickable {
                                        downloadStates[msg.id] = DownloadState.NotDownloaded
                                        onDownload(msg, File(""))
                                    }
                                ) {
                                    Text(
                                        text = "${state.msg} · 点击重试",
                                        color = if (isMe) TextWhite.copy(alpha = 0.7f) else UnreadRed,
                                        fontSize = 12.sp
                                    )
                                }
                            }
                        }

                        Spacer(modifier = Modifier.height(4.dp))
                        Text(
                            text = timeFmt.format(Date(msg.createdAt)),
                            color = textColor.copy(alpha = 0.5f),
                            fontSize = 10.sp
                        )
                    }
                }
            }
        }
    }
}

fun formatFileSize(bytes: Long): String {
    return when {
        bytes < 1024 -> "$bytes B"
        bytes < 1024 * 1024 -> "%.1f KB".format(bytes / 1024.0)
        bytes < 1024 * 1024 * 1024 -> "%.1f MB".format(bytes / 1024.0 / 1024.0)
        else -> "%.1f GB".format(bytes / 1024.0 / 1024.0 / 1024.0)
    }
}
