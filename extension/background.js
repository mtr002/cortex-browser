console.log('Background script loaded.');

let ws = null;
let reconnectInterval = null;

function connectWebSocket() {
  try {
    ws = new WebSocket('ws://localhost:8080/ws');
    
    ws.onopen = function(event) {
      console.log('WebSocket Connection Opened!');
      clearInterval(reconnectInterval);
      
      // Send a test message to verify connection
      ws.send('Hello from the extension!');
    };
    
    ws.onmessage = function(event) {
      console.log('Message received from server:', event.data);
      // TODO: Forward commands to content scripts in future phases
    };
    
    ws.onclose = function(event) {
      console.log('WebSocket Connection Closed:', event.code, event.reason);
      attemptReconnect();
    };
    
    ws.onerror = function(error) {
      console.error('WebSocket Error:', error);
    };
    
  } catch (error) {
    console.error('Failed to create WebSocket connection:', error);
    attemptReconnect();
  }
}

function attemptReconnect() {
  if (reconnectInterval) return; // Already attempting to reconnect
  
  console.log('Attempting to reconnect in 3 seconds...');
  reconnectInterval = setInterval(() => {
    if (ws === null || ws.readyState === WebSocket.CLOSED) {
      connectWebSocket();
    }
  }, 3000);
}

// Initialize connection when background script loads
connectWebSocket();

// Listen for extension messages (for future phases)
chrome.runtime.onMessage.addListener((message, sender, sendResponse) => {
  console.log('Received message from extension:', message);
  
  // Forward message to WebSocket server if connected
  if (ws && ws.readyState === WebSocket.OPEN) {
    ws.send(JSON.stringify(message));
  } else {
    console.warn('WebSocket not connected, cannot send message');
  }
});
