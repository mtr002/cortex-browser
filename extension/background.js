console.log('Background script loaded.');

// WebSocket connection state
let ws = null;
let reconnectInterval = null;
let isConnected = false;

// Active tasks tracking
let activeTasks = new Map();

// Initialize
connectWebSocket();

function connectWebSocket() {
  try {
    ws = new WebSocket('ws://localhost:8080/ws');
    
    ws.onopen = function(event) {
      console.log('WebSocket Connection Opened!');
      clearInterval(reconnectInterval);
      isConnected = true;
      
      // Notify sidepanel of connection status
      notifyConnectionStatus('connected', 'Connected to backend');
      
      // Send initial handshake
      sendToBackend({
        type: 'HANDSHAKE',
        payload: { 
          client: 'extension',
          version: chrome.runtime.getManifest().version
        }
      });
    };
    
    ws.onmessage = function(event) {
      console.log('Message received from backend:', event.data);
      try {
        const message = JSON.parse(event.data);
        handleBackendMessage(message);
      } catch (error) {
        console.error('Failed to parse backend message:', error);
      }
    };
    
    ws.onclose = function(event) {
      console.log('WebSocket Connection Closed:', event.code, event.reason);
      isConnected = false;
      notifyConnectionStatus('disconnected', 'Disconnected from backend');
      attemptReconnect();
    };
    
    ws.onerror = function(error) {
      console.error('WebSocket Error:', error);
      isConnected = false;
      notifyConnectionStatus('disconnected', 'Connection error');
    };
    
  } catch (error) {
    console.error('Failed to create WebSocket connection:', error);
    attemptReconnect();
  }
}

function attemptReconnect() {
  if (reconnectInterval) return;
  
  console.log('Attempting to reconnect in 3 seconds...');
  notifyConnectionStatus('connecting', 'Reconnecting...');
  
  reconnectInterval = setInterval(() => {
    if (ws === null || ws.readyState === WebSocket.CLOSED) {
      connectWebSocket();
    }
  }, 3000);
}

function sendToBackend(message) {
  if (ws && ws.readyState === WebSocket.OPEN) {
    ws.send(JSON.stringify(message));
    console.log('Sent to backend:', message);
    return true;
  } else {
    console.warn('WebSocket not connected, cannot send message');
    return false;
  }
}

function handleBackendMessage(message) {
  switch (message.type) {
    case 'COMMAND':
      executeCommand(message.payload);
      break;
    case 'TASK_COMPLETE':
      handleTaskComplete(message.payload);
      break;
    case 'ERROR':
      handleBackendError(message.payload);
      break;
    default:
      console.log('Unknown backend message type:', message.type);
  }
}

async function executeCommand(command) {
  console.log('Executing command:', command);
  
  try {
    const [activeTab] = await chrome.tabs.query({ active: true, currentWindow: true });
    if (!activeTab) {
      throw new Error('No active tab found');
    }

    let result;
    
    switch (command.action) {
      case 'navigate':
        result = await handleNavigateCommand(activeTab, command);
        break;
      case 'click':
      case 'input':
      case 'get_content':
        result = await sendCommandToContent(activeTab, command);
        break;
      default:
        throw new Error(`Unknown command action: ${command.action}`);
    }

    // Notify sidepanel of successful execution
    notifySidepanel('COMMAND_EXECUTED', {
      action: command.action,
      details: result?.details || null
    });

  } catch (error) {
    console.error('Command execution failed:', error);
    
    // Notify sidepanel of failure
    notifySidepanel('COMMAND_FAILED', {
      action: command.action,
      error: error.message
    });
  }
}

async function handleNavigateCommand(tab, command) {
  await chrome.tabs.update(tab.id, { url: command.url });
  return { details: `Navigated to ${command.url}` };
}

async function sendCommandToContent(tab, command) {
  return new Promise((resolve, reject) => {
    chrome.tabs.sendMessage(tab.id, {
      type: 'EXECUTE_COMMAND',
      payload: command
    }, (response) => {
      if (chrome.runtime.lastError) {
        reject(new Error(chrome.runtime.lastError.message));
      } else if (response?.success) {
        resolve(response);
      } else {
        reject(new Error(response?.error || 'Command execution failed'));
      }
    });
  });
}

function handleTaskComplete(payload) {
  notifySidepanel('EXECUTION_COMPLETE', payload);
}

function handleBackendError(payload) {
  notifySidepanel('COMMAND_FAILED', {
    action: 'backend_processing',
    error: payload.message || 'Backend error occurred'
  });
}

function notifyConnectionStatus(status, message) {
  notifySidepanel('CONNECTION_STATUS', { status, message });
}

function notifySidepanel(type, payload) {
  chrome.runtime.sendMessage({
    type: type,
    payload: payload
  }).catch(error => {
    // Sidepanel might not be open, this is okay
    console.log('Could not notify sidepanel:', error.message);
  });
}

// Handle messages from extension components
chrome.runtime.onMessage.addListener((message, sender, sendResponse) => {
  console.log('Received message:', message, 'from:', sender);

  switch (message.type) {
    case 'CHECK_CONNECTION':
      sendResponse({ connected: isConnected });
      break;

    case 'EXECUTE_TASK':
      if (!isConnected) {
        sendResponse({ status: 'error', message: 'Backend not connected' });
        return;
      }
      
      const success = sendToBackend(message);
      sendResponse({ status: success ? 'sent' : 'failed' });
      break;

    default:
      // Forward other messages to backend
      if (isConnected) {
        sendToBackend(message);
        sendResponse({ status: 'forwarded' });
      } else {
        sendResponse({ status: 'error', message: 'Backend not connected' });
      }
  }

  return true; // Keep channel open for async response
});
