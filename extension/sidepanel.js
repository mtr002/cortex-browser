console.log('Sidepanel script loaded');

// Global error handlers to prevent crashes
window.addEventListener('error', (event) => {
  console.error('Global error in sidepanel:', event.error);
  // Show user-friendly error message
  if (feedbackContent) {
    const errorItem = document.createElement('div');
    errorItem.className = 'feedback-item';
    errorItem.innerHTML = `
      <div class="feedback-icon error">❌</div>
      <div class="feedback-text">
        <div>Error occurred</div>
        <div class="feedback-details">${event.error?.message || 'Unknown error'}</div>
      </div>
    `;
    feedbackContent.appendChild(errorItem);
  }
  event.preventDefault();
});

window.addEventListener('unhandledrejection', (event) => {
  console.error('Unhandled promise rejection in sidepanel:', event.reason);
  event.preventDefault();
});

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
let currentSequence = null;
let connectionCheckInterval = null; // Store interval ID for cleanup

// Initialize
document.addEventListener('DOMContentLoaded', () => {
    setupEventListeners();
    checkBackgroundConnection();
    setupTextareaResize();
});

// Cleanup on page unload
window.addEventListener('beforeunload', () => {
    if (connectionCheckInterval) {
        clearInterval(connectionCheckInterval);
        connectionCheckInterval = null;
    }
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
    // Clear existing interval if it exists to prevent duplicates
    if (connectionCheckInterval) {
        clearInterval(connectionCheckInterval);
    }
    
    // Check if background script is ready and connected to backend
    try {
        chrome.runtime.sendMessage({ type: 'CHECK_CONNECTION' }, (response) => {
            if (chrome.runtime.lastError) {
                console.error('Connection check error:', chrome.runtime.lastError);
                updateConnectionStatus('disconnected', 'Background script error');
                return;
            }
            
            updateConnectionStatus(
                response?.connected ? 'connected' : 'disconnected',
                response?.connected ? 'Backend connected' : 'Backend disconnected'
            );
        });
    } catch (error) {
        console.error('Failed to check connection:', error);
        updateConnectionStatus('disconnected', 'Connection check failed');
    }

    // Check periodically - only set once
    if (!connectionCheckInterval) {
        connectionCheckInterval = setInterval(checkBackgroundConnection, 5000);
    }
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
            
        case 'SEQUENCE_STARTED':
            handleSequenceStarted(message.payload);
            break;
            
        case 'SEQUENCE_UPDATE':
            handleSequenceUpdate(message.payload);
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
    currentSequence = null;
}

function handleSequenceStarted(sequence) {
    console.log('Sequence started:', sequence);
    currentSequence = sequence;
    showExecutionFeedback();
    
    // Create timeline visualization
    const timelineDiv = document.createElement('div');
    timelineDiv.className = 'sequence-timeline';
    timelineDiv.id = 'sequenceTimeline';
    
    let timelineHTML = '<div class="timeline-header">📋 Multi-Step Task</div>';
    timelineHTML += '<div class="timeline-steps">';
    
    sequence.commands.forEach((cmd, index) => {
        const stepClass = index === 0 ? 'active' : 'pending';
        timelineHTML += `
            <div class="timeline-step ${stepClass}" data-step="${index}">
                <div class="step-number">${index + 1}</div>
                <div class="step-content">
                    <div class="step-action">${getActionLabel(cmd.action)}</div>
                    <div class="step-details">${getStepDetails(cmd)}</div>
                </div>
            </div>
        `;
    });
    
    timelineHTML += '</div>';
    timelineDiv.innerHTML = timelineHTML;
    
    // Clear existing feedback and add timeline
    feedbackContent.innerHTML = '';
    feedbackContent.appendChild(timelineDiv);
}

function handleSequenceUpdate(sequence) {
    console.log('Sequence update:', sequence);
    currentSequence = sequence;
    
    const timeline = document.getElementById('sequenceTimeline');
    if (!timeline) return;
    
    // Update step states
    const steps = timeline.querySelectorAll('.timeline-step');
    steps.forEach((step, index) => {
        const stepNum = parseInt(step.dataset.step);
        step.classList.remove('active', 'completed', 'pending');
        
        if (stepNum < sequence.current) {
            step.classList.add('completed');
        } else if (stepNum === sequence.current) {
            step.classList.add('active');
        } else {
            step.classList.add('pending');
        }
    });
}

function getActionLabel(action) {
    const labels = {
        'navigate': '🌐 Navigate',
        'input': '⌨️ Input',
        'click': '👆 Click',
        'get_content': '📄 Get Content'
    };
    return labels[action] || action;
}

function getStepDetails(command) {
    if (command.action === 'navigate') {
        return command.url || 'Navigate to URL';
    } else if (command.action === 'input') {
        return `Type: "${command.text || ''}"`;
    } else if (command.action === 'click') {
        return `Click: ${command.selector || 'element'}`;
    } else if (command.action === 'get_content') {
        return 'Extract page content';
    }
    return '';
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
