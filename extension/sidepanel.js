console.log('Sidepanel script loaded');

// DOM Elements
const statusDot = document.getElementById('statusDot');
const goalInput = document.getElementById('goalInput');
const submitBtn = document.getElementById('submitBtn');
const welcomeMessage = document.getElementById('welcomeMessage');
const executionFeedback = document.getElementById('executionFeedback');
const feedbackContent = document.getElementById('feedbackContent');

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
    showExecutionFeedback();
    addFeedbackItem('🚀', 'Sending goal to backend...', goal, 'executing');

    // Send goal to background script
    const message = {
        type: 'EXECUTE_TASK',
        payload: { goal: goal }
    };

    chrome.runtime.sendMessage(message, (response) => {
        if (chrome.runtime.lastError) {
            console.error('Failed to send goal:', chrome.runtime.lastError.message);
            addFeedbackItem('❌', 'Failed to send goal', chrome.runtime.lastError.message, 'error');
            setExecutionState(false);
            return;
        }

        if (response?.status === 'sent') {
            addFeedbackItem('✓', 'Goal sent to backend', 'Waiting for command...', 'success');
        } else {
            addFeedbackItem('❌', 'Failed to send goal to backend', response?.message || 'Unknown error', 'error');
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
            const action = message.payload.action;
            const details = message.payload.details;
            const elementsFound = message.payload.elementsFound;
            
            addFeedbackItem('✅', `Executed: ${action}`, details, 'success');
            
            if (elementsFound) {
                const elementSummary = `Found ${elementsFound.inputs} inputs, ${elementsFound.buttons} buttons, ${elementsFound.links} links`;
                addFeedbackItem('📊', 'Page Analysis', elementSummary, 'success');
            }
            break;
            
        case 'COMMAND_FAILED':
            console.error('Command failed:', message.payload);
            addFeedbackItem('❌', `Failed: ${message.payload.action}`, message.payload.error, 'error');
            setExecutionState(false);
            break;
            
        case 'EXECUTION_COMPLETE':
            console.log('Execution complete:', message.payload);
            addFeedbackItem('🎉', 'Task Completed', message.payload.message || 'Task finished successfully', 'success');
            setExecutionState(false);
            break;
            
        case 'CONTENT_ANALYSIS':
            handleContentAnalysisResult(message.payload);
            break;
            
        case 'CONNECTION_STATUS':
            updateConnectionStatus(message.payload.status, message.payload.message);
            break;
            
        default:
            console.log('Received message:', message.type);
    }

    sendResponse({ status: 'received' });
});

// New functions for enhanced feedback
function showExecutionFeedback() {
    welcomeMessage.style.display = 'none';
    executionFeedback.style.display = 'block';
    feedbackContent.innerHTML = ''; // Clear previous feedback
}

function hideExecutionFeedback() {
    welcomeMessage.style.display = 'block';
    executionFeedback.style.display = 'none';
}

function addFeedbackItem(icon, title, details, type = 'info') {
    const feedbackItem = document.createElement('div');
    feedbackItem.className = 'feedback-item';
    
    feedbackItem.innerHTML = `
        <div class="feedback-icon ${type}">${icon}</div>
        <div class="feedback-text">
            <div>${title}</div>
            ${details ? `<div class="feedback-details">${details}</div>` : ''}
        </div>
    `;
    
    feedbackContent.appendChild(feedbackItem);
    
    // Auto-scroll to bottom
    feedbackContent.scrollTop = feedbackContent.scrollHeight;
}

function handleContentAnalysisResult(analysis) {
    console.log('Content analysis result:', analysis);
    
    // Create analysis display
    const analysisDiv = document.createElement('div');
    analysisDiv.className = 'page-analysis';
    
    let analysisHTML = '<div class="page-analysis-title">🔍 Page Analysis</div>';
    
    if (analysis.contentType) {
        analysisHTML += `<div class="page-analysis-item">Type: ${analysis.contentType}</div>`;
    }
    
    if (analysis.selectors && analysis.selectors.length > 0) {
        analysisHTML += `<div class="page-analysis-item">Interactive elements: ${analysis.selectors.length}</div>`;
    }
    
    if (analysis.suggestions && analysis.suggestions.length > 0) {
        analysisHTML += `<div class="page-analysis-item">Suggestions:</div>`;
        analysis.suggestions.forEach(suggestion => {
            analysisHTML += `<div class="page-analysis-item">• ${suggestion}</div>`;
        });
    }
    
    analysisDiv.innerHTML = analysisHTML;
    feedbackContent.appendChild(analysisDiv);
    
    // Auto-scroll to bottom
    feedbackContent.scrollTop = feedbackContent.scrollHeight;
}

function clearFeedback() {
    feedbackContent.innerHTML = '';
    hideExecutionFeedback();
}

// Export for debugging
window.sidepanelDebug = {
    updateConnectionStatus,
    setExecutionState,
    addFeedbackItem,
    showExecutionFeedback,
    hideExecutionFeedback,
    clearFeedback,
    isConnected: () => isConnected,
    isExecuting: () => isExecuting
};
