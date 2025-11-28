console.log('Background script loaded.');

// Global error handlers to prevent crashes
self.addEventListener('error', (event) => {
  console.error('Global error in background script:', event.error);
  // Prevent the error from crashing the extension
  event.preventDefault();
});

self.addEventListener('unhandledrejection', (event) => {
  console.error('Unhandled promise rejection in background script:', event.reason);
  // Prevent the rejection from crashing the extension
  event.preventDefault();
});

// WebSocket connection state
let ws = null;
let reconnectInterval = null;
let isConnected = false;

// Active tasks tracking
let activeTasks = new Map();
let currentSequence = null;

// Initialize with error handling
try {
  connectWebSocket();
} catch (error) {
  console.error('Failed to initialize WebSocket connection:', error);
  // Will attempt to reconnect automatically
}

function connectWebSocket() {
  try {
    // Clean up existing connection before creating new one
    if (ws) {
      try {
        ws.onopen = null;
        ws.onmessage = null;
        ws.onclose = null;
        ws.onerror = null;
        if (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING) {
          ws.close();
        }
      } catch (e) {
        console.error('Error cleaning up old WebSocket:', e);
      }
      ws = null;
    }
    
    ws = new WebSocket('ws://localhost:8080/ws');
    
    ws.onopen = function(event) {
      console.log('WebSocket Connection Opened!');
      if (reconnectInterval) {
        clearInterval(reconnectInterval);
        reconnectInterval = null;
      }
      isConnected = true;
      
      // Notify sidepanel of connection status
      notifyConnectionStatus('connected', 'Connected to backend');
      
      // Send initial handshake
      try {
        sendToBackend({
          type: 'HANDSHAKE',
          payload: { 
            client: 'extension',
            version: chrome.runtime.getManifest().version
          }
        });
      } catch (error) {
        console.error('Failed to send handshake:', error);
      }
    };
    
    ws.onmessage = function(event) {
      try {
        console.log('Message received from backend:', event.data);
        const message = JSON.parse(event.data);
        handleBackendMessage(message);
      } catch (error) {
        console.error('Failed to parse backend message:', error, event.data);
        // Don't crash on parse errors, just log them
      }
    };
    
    ws.onclose = function(event) {
      console.log('WebSocket Connection Closed:', event.code, event.reason);
      isConnected = false;
      notifyConnectionStatus('disconnected', 'Disconnected from backend');
      
      // Only attempt reconnect if it wasn't a manual close
      if (event.code !== 1000) {
        attemptReconnect();
      }
    };
    
    ws.onerror = function(error) {
      console.error('WebSocket Error:', error);
      isConnected = false;
      notifyConnectionStatus('disconnected', 'Connection error');
      // Don't attempt reconnect here - let onclose handle it
    };
    
  } catch (error) {
    console.error('Failed to create WebSocket connection:', error);
    isConnected = false;
    notifyConnectionStatus('disconnected', 'Failed to connect');
    attemptReconnect();
  }
}

function attemptReconnect() {
  // Clear any existing reconnect interval to prevent duplicates
  if (reconnectInterval) {
    clearInterval(reconnectInterval);
    reconnectInterval = null;
  }
  
  console.log('Attempting to reconnect in 3 seconds...');
  notifyConnectionStatus('connecting', 'Reconnecting...');
  
  reconnectInterval = setInterval(() => {
    // Only reconnect if WebSocket is actually closed/null
    if (!ws || ws.readyState === WebSocket.CLOSED || ws.readyState === WebSocket.CLOSING) {
      // Close any existing connection before creating new one
      if (ws) {
        try {
          ws.close();
        } catch (e) {
          console.error('Error closing old WebSocket:', e);
        }
        ws = null;
      }
      connectWebSocket();
    } else if (ws.readyState === WebSocket.OPEN) {
      // Connection is open, stop trying to reconnect
      clearInterval(reconnectInterval);
      reconnectInterval = null;
      isConnected = true;
      notifyConnectionStatus('connected', 'Connected to backend');
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
  try {
    if (!message || !message.type) {
      console.warn('Received invalid message from backend:', message);
      return;
    }
    
    switch (message.type) {
      case 'COMMAND':
        if (message.payload) {
          executeCommand(message.payload).catch(error => {
            console.error('Error executing command:', error);
          });
        } else {
          console.warn('COMMAND message missing payload');
        }
        break;
      case 'COMMAND_SEQUENCE':
        handleCommandSequence(message.payload);
        break;
      case 'COMMAND_SEQUENCE_UPDATE':
        handleSequenceUpdate(message.payload);
        break;
      case 'TASK_COMPLETE':
        handleTaskComplete(message.payload);
        break;
      case 'ERROR':
        handleBackendError(message.payload);
        break;
      case 'CONTENT_ANALYSIS':
        handleContentAnalysis(message.payload);
        break;
      default:
        console.log('Unknown backend message type:', message.type);
    }
  } catch (error) {
    console.error('Error handling backend message:', error, message);
    // Don't crash on message handling errors
  }
}

async function executeCommand(command) {
  console.log('Executing command:', command);
  
  try {
    // Validate command
    if (!command || !command.action) {
      throw new Error('Invalid command: missing action');
    }
    
    const [activeTab] = await chrome.tabs.query({ active: true, currentWindow: true }).catch(error => {
      console.error('Error querying tabs:', error);
      throw new Error('Failed to get active tab');
    });
    
    if (!activeTab) {
      throw new Error('No active tab found');
    }

    let result;
    
    try {
      switch (command.action) {
        case 'navigate':
          // Navigation is allowed even from restricted pages (we're navigating away)
          result = await handleNavigateCommand(activeTab, command);
          break;
        case 'click':
        case 'input':
        case 'get_content':
          // Refresh tab info in case we just navigated
          const [refreshedTab] = await chrome.tabs.query({ active: true, currentWindow: true });
          const tabToUse = refreshedTab || activeTab;
          
          // Check if tab URL is accessible for content script commands
          if (tabToUse.url && (
            tabToUse.url.startsWith('chrome://') || 
            tabToUse.url.startsWith('chrome-extension://') ||
            tabToUse.url.startsWith('about:')
          )) {
            throw new Error(`Cannot execute commands on ${tabToUse.url} pages`);
          }
          result = await sendCommandToContent(tabToUse, command);
          break;
        default:
          throw new Error(`Unknown command action: ${command.action}`);
      }
    } catch (actionError) {
      console.error(`Error in ${command.action} command:`, actionError);
      throw actionError;
    }

    // Notify sidepanel of successful execution
    try {
      notifySidepanel('COMMAND_EXECUTED', {
        action: command.action,
        details: result?.details || null,
        elementsFound: result?.elementsFound || null
      });
    } catch (notifyError) {
      console.warn('Failed to notify sidepanel:', notifyError);
      // Don't fail the command if notification fails
    }

    // Send command completion to backend (for multi-step sequences)
    if (currentSequence) {
      try {
        sendToBackend({
          type: 'COMMAND_COMPLETE',
          payload: {
            step: currentSequence.current || 0,
            action: command.action,
            success: true,
            details: result?.details || 'Command executed successfully',
            timestamp: new Date().toISOString()
          }
        });
      } catch (backendError) {
        console.warn('Failed to send completion to backend:', backendError);
        // Don't fail the command if backend notification fails
      }
    }

  } catch (error) {
    console.error('Command execution failed:', error);
    
    // Notify sidepanel of failure
    try {
      notifySidepanel('COMMAND_FAILED', {
        action: command?.action || 'unknown',
        error: error.message || 'Unknown error occurred'
      });
    } catch (notifyError) {
      console.warn('Failed to notify sidepanel of error:', notifyError);
    }

    // Send failure to backend
    if (currentSequence) {
      try {
        sendToBackend({
          type: 'COMMAND_COMPLETE',
          payload: {
            step: currentSequence.current || 0,
            action: command?.action || 'unknown',
            success: false,
            error: error.message || 'Unknown error occurred',
            timestamp: new Date().toISOString()
          }
        });
      } catch (backendError) {
        console.warn('Failed to send error to backend:', backendError);
      }
    }
  }
}

async function handleNavigateCommand(tab, command) {
  // Update the tab URL
  await chrome.tabs.update(tab.id, { url: command.url });
  
  // Wait for the page to finish loading
  return new Promise((resolve, reject) => {
    const timeout = setTimeout(() => {
      chrome.tabs.onUpdated.removeListener(listener);
      reject(new Error('Navigation timeout: page did not load within 15 seconds'));
    }, 15000);
    
    // Listen for tab update to detect when page loads
    const listener = (tabId, changeInfo, updatedTab) => {
      // Only process updates for our tab
      if (tabId !== tab.id) return;
      
      // Check if page is fully loaded
      if (changeInfo.status === 'complete') {
        clearTimeout(timeout);
        chrome.tabs.onUpdated.removeListener(listener);
        
        // Ensure we're not on a restricted page
        if (updatedTab.url && (
          updatedTab.url.startsWith('chrome://') || 
          updatedTab.url.startsWith('chrome-extension://') ||
          updatedTab.url.startsWith('about:')
        )) {
          reject(new Error(`Cannot navigate to restricted page: ${updatedTab.url}`));
          return;
        }
        
        // Small delay to ensure page is ready and content script can attach
        setTimeout(() => {
          resolve({ details: `Navigated to ${command.url}` });
        }, 1000); // Increased delay to ensure page is ready
      }
    };
    
    chrome.tabs.onUpdated.addListener(listener);
  });
}

async function sendCommandToContent(tab, command) {
  try {
    // First, ensure content script is injected
    await ensureContentScriptInjected(tab.id);
    
    return new Promise((resolve, reject) => {
      // Set timeout to prevent hanging indefinitely
      const timeout = setTimeout(() => {
        reject(new Error('Command execution timeout (30s)'));
      }, 30000);
      
      try {
        chrome.tabs.sendMessage(tab.id, {
          type: 'EXECUTE_COMMAND',
          payload: command
        }, (response) => {
          clearTimeout(timeout);
          
          if (chrome.runtime.lastError) {
            console.error('Content script message error:', chrome.runtime.lastError.message);
            reject(new Error(`Content script error: ${chrome.runtime.lastError.message}`));
            return;
          }
          
          if (response?.success) {
            resolve(response);
          } else {
            reject(new Error(response?.error || 'Command execution failed'));
          }
        });
      } catch (error) {
        clearTimeout(timeout);
        reject(new Error(`Failed to send message to content script: ${error.message}`));
      }
    });
  } catch (error) {
    throw new Error(`Failed to communicate with content script: ${error.message}`);
  }
}

async function ensureContentScriptInjected(tabId) {
  try {
    // Test if content script is already available (it should be via manifest.json)
    // Use a timeout to prevent hanging if content script isn't responding
    const response = await Promise.race([
      chrome.tabs.sendMessage(tabId, { type: 'PING' }),
      new Promise((_, reject) => setTimeout(() => reject(new Error('Timeout')), 2000))
    ]);
    
    if (response && response.status === 'ready') {
      return; // Content script is ready
    }
  } catch (error) {
    // Content script might not be loaded yet (e.g., on chrome:// pages)
    // Only inject if it's a regular web page
    try {
      const tab = await chrome.tabs.get(tabId);
      const url = new URL(tab.url);
      
      // Don't inject on chrome://, chrome-extension://, or other special pages
      if (url.protocol === 'chrome:' || url.protocol === 'chrome-extension:' || 
          url.protocol === 'chrome-search:' || url.protocol === 'about:') {
        throw new Error(`Cannot inject content script on ${url.protocol} pages`);
      }
      
      console.log('Content script not found, attempting to inject...');
      await chrome.scripting.executeScript({
        target: { tabId: tabId },
        files: ['content.js']
      });
      
      // Wait a moment for the script to initialize
      await new Promise(resolve => setTimeout(resolve, 200));
      
      // Verify it's now available
      const verifyResponse = await Promise.race([
        chrome.tabs.sendMessage(tabId, { type: 'PING' }),
        new Promise((_, reject) => setTimeout(() => reject(new Error('Timeout')), 1000))
      ]);
      
      if (verifyResponse && verifyResponse.status === 'ready') {
        console.log('Content script injected successfully');
        return;
      } else {
        throw new Error('Content script injection verification failed');
      }
    } catch (injectionError) {
      console.error('Failed to inject content script:', injectionError);
      throw new Error(`Content script unavailable: ${injectionError.message}`);
    }
  }
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

function handleContentAnalysis(payload) {
  console.log('Content analysis received:', payload);
  notifySidepanel('CONTENT_ANALYSIS', payload);
}

function handleCommandSequence(sequence) {
  console.log('Command sequence received:', sequence);
  currentSequence = sequence;
  notifySidepanel('SEQUENCE_STARTED', sequence);
}

function handleSequenceUpdate(sequence) {
  console.log('Sequence update received:', sequence);
  currentSequence = sequence;
  notifySidepanel('SEQUENCE_UPDATE', sequence);
}

function notifyConnectionStatus(status, message) {
  notifySidepanel('CONNECTION_STATUS', { status, message });
}

function notifySidepanel(type, payload) {
  try {
    chrome.runtime.sendMessage({
      type: type,
      payload: payload
    }).catch(error => {
      // Sidepanel might not be open, this is okay
      console.log('Could not notify sidepanel:', error.message);
    });
  } catch (error) {
    // Silently fail - sidepanel might not be available
    console.log('Failed to send message to sidepanel:', error.message);
  }
}

// Handle messages from extension components
chrome.runtime.onMessage.addListener((message, sender, sendResponse) => {
  try {
    console.log('Received message:', message, 'from:', sender);

    // Validate message
    if (!message || !message.type) {
      sendResponse({ status: 'error', message: 'Invalid message format' });
      return true;
    }

    switch (message.type) {
      case 'CHECK_CONNECTION':
        sendResponse({ connected: isConnected });
        break;

      case 'EXECUTE_TASK':
        if (!isConnected) {
          sendResponse({ status: 'error', message: 'Backend not connected' });
          return true;
        }
        
        try {
          const success = sendToBackend(message);
          sendResponse({ status: success ? 'sent' : 'failed' });
        } catch (error) {
          console.error('Error sending task to backend:', error);
          sendResponse({ status: 'error', message: error.message || 'Failed to send task' });
        }
        break;
        
      case 'PAGE_CONTENT':
        if (!isConnected) {
          sendResponse({ status: 'error', message: 'Backend not connected' });
          return true;
        }
        
        try {
          // Limit payload size to prevent memory issues
          if (message.payload && message.payload.html) {
            // Don't send full HTML if it exists
            delete message.payload.html;
          }
          
          const contentSuccess = sendToBackend(message);
          sendResponse({ status: contentSuccess ? 'sent' : 'failed' });
        } catch (error) {
          console.error('Error sending page content to backend:', error);
          sendResponse({ status: 'error', message: error.message || 'Failed to send content' });
        }
        break;

      default:
        // Forward other messages to backend
        try {
          if (isConnected) {
            sendToBackend(message);
            sendResponse({ status: 'forwarded' });
          } else {
            sendResponse({ status: 'error', message: 'Backend not connected' });
          }
        } catch (error) {
          console.error('Error forwarding message:', error);
          sendResponse({ status: 'error', message: error.message || 'Failed to forward message' });
        }
    }

    return true; // Keep channel open for async response
  } catch (error) {
    console.error('Error in message listener:', error);
    try {
      sendResponse({ status: 'error', message: error.message || 'Unknown error' });
    } catch (responseError) {
      // Response may have already been sent
      console.error('Failed to send error response:', responseError);
    }
    return true;
  }
});
