console.log('Sidepanel script loaded');

// DOM Elements
const statusDot = document.getElementById('statusDot');
const goalInput = document.getElementById('goalInput');
const submitBtn = document.getElementById('submitBtn');

// State
let isConnected = false;
let isExecuting = false;

// Initialize
document.addEventListener('DOMContentLoaded', () => {
    setupEventListeners();
    checkBackgroundConnection();
    setupTextareaResize();
});

function setupEventListeners() {
    // Submit goal
    submitBtn.addEventListener('click', handleSubmitGoal);
    
    // Handle Enter key (Shift+Enter for new line, Enter to submit)
    goalInput.addEventListener('keydown', (e) => {
        if (e.key === 'Enter' && !e.shiftKey && !submitBtn.disabled) {
            e.preventDefault();
            handleSubmitGoal();
        }
    });

    // Enable/disable submit based on input
    goalInput.addEventListener('input', (e) => {
        const hasText = e.target.value.trim().length > 0;
        const canSubmit = hasText && isConnected && !isExecuting;
        submitBtn.disabled = !canSubmit;
    });
}

function setupTextareaResize() {
    goalInput.addEventListener('input', () => {
        goalInput.style.height = 'auto';
        goalInput.style.height = Math.min(goalInput.scrollHeight, 120) + 'px';
    });
}

function checkBackgroundConnection() {
    // Check if background script is ready and connected to backend
    chrome.runtime.sendMessage({ type: 'CHECK_CONNECTION' }, (response) => {
        if (chrome.runtime.lastError) {
            updateConnectionStatus('disconnected', 'Background script error');
            return;
        }
        
        updateConnectionStatus(
            response?.connected ? 'connected' : 'disconnected',
            response?.connected ? 'Backend connected' : 'Backend disconnected'
        );
    });

    // Check periodically
    setInterval(checkBackgroundConnection, 5000);
}

function updateConnectionStatus(status, message) {
    isConnected = status === 'connected';
    
    // Update status dot
    statusDot.className = 'status-dot';
    if (status === 'connected') {
        statusDot.classList.add('connected');
    } else if (status === 'connecting') {
        statusDot.classList.add('connecting');
    }
    
    // Update submit button availability
    const hasText = goalInput.value.trim().length > 0;
    const canSubmit = hasText && isConnected && !isExecuting;
    submitBtn.disabled = !canSubmit;
}

function handleSubmitGoal() {
    const goal = goalInput.value.trim();
    if (!goal || !isConnected || isExecuting) return;

    setExecutionState(true);

    // Send goal to background script
    const message = {
        type: 'EXECUTE_TASK',
        payload: { goal: goal }
    };

    chrome.runtime.sendMessage(message, (response) => {
        if (chrome.runtime.lastError) {
            console.error('Failed to send goal:', chrome.runtime.lastError.message);
            setExecutionState(false);
            return;
        }

        if (response?.status !== 'sent') {
            console.error('Failed to send goal to backend');
            setExecutionState(false);
        }
    });

    goalInput.value = '';
    // Reset textarea height
    goalInput.style.height = '20px';
}

function setExecutionState(executing) {
    isExecuting = executing;
    
    // Update button state
    if (executing) {
        submitBtn.classList.add('executing');
    } else {
        submitBtn.classList.remove('executing');
    }
    
    submitBtn.disabled = executing || !isConnected || goalInput.value.trim().length === 0;
    goalInput.disabled = executing;
}

// Listen for messages from background script
chrome.runtime.onMessage.addListener((message, sender, sendResponse) => {
    console.log('Sidepanel received message:', message);

    switch (message.type) {
        case 'COMMAND_EXECUTED':
            console.log('Command executed:', message.payload);
            break;
        case 'COMMAND_FAILED':
            console.error('Command failed:', message.payload);
            setExecutionState(false);
            break;
        case 'EXECUTION_COMPLETE':
            setExecutionState(false);
            console.log('Execution complete:', message.payload);
            break;
        case 'CONNECTION_STATUS':
            updateConnectionStatus(message.payload.status, message.payload.message);
            break;
        default:
            console.log('Received message:', message.type);
    }

    sendResponse({ status: 'received' });
});

// Export for debugging
window.sidepanelDebug = {
    updateConnectionStatus,
    setExecutionState,
    isConnected: () => isConnected,
    isExecuting: () => isExecuting
};
