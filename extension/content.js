console.log('Content script loaded on:', window.location.href);

// Listen for messages from background script
chrome.runtime.onMessage.addListener((message, sender, sendResponse) => {
  console.log('Content script received message:', message);
  
  if (message.type === 'EXECUTE_COMMAND') {
    executeCommand(message.payload)
      .then(result => {
        console.log('Command executed successfully:', result);
        sendResponse({
          success: true,
          ...result
        });
      })
      .catch(error => {
        console.error('Command execution failed:', error);
        sendResponse({
          success: false,
          error: error.message
        });
      });
    
    return true; // Keep channel open for async response
  }
  
  sendResponse({status: 'received'});
});

async function executeCommand(command) {
  console.log('Executing command:', command);
  
  switch (command.action) {
    case 'click':
      return await executeClickCommand(command);
    case 'input':
      return await executeInputCommand(command);
    case 'get_content':
      return await executeGetContentCommand(command);
    default:
      throw new Error(`Unknown command action: ${command.action}`);
  }
}

async function executeClickCommand(command) {
  if (!command.selector) {
    throw new Error('Click command requires selector');
  }

  const element = findElement(command.selector);
  if (!element) {
    throw new Error(`Element not found: ${command.selector}`);
  }

  // Wait for element to be visible and interactable
  await waitForElementReady(element);
  
  // Scroll element into view
  element.scrollIntoView({ behavior: 'smooth', block: 'center' });
  
  // Wait a bit for scroll to complete
  await sleep(500);
  
  // Click the element
  element.click();
  
  return {
    details: `Clicked element: ${command.selector}`,
    elementText: element.textContent?.trim().substring(0, 50) || '',
    elementTag: element.tagName.toLowerCase()
  };
}

async function executeInputCommand(command) {
  if (!command.selector || !command.text) {
    throw new Error('Input command requires selector and text');
  }

  const element = findElement(command.selector);
  if (!element) {
    throw new Error(`Input element not found: ${command.selector}`);
  }

  // Wait for element to be ready
  await waitForElementReady(element);
  
  // Focus the element
  element.focus();
  
  // Clear existing content
  if (element.value !== undefined) {
    element.value = '';
  } else {
    element.textContent = '';
  }
  
  // Type the text with a natural delay
  await typeText(element, command.text);
  
  // Trigger input events
  element.dispatchEvent(new Event('input', { bubbles: true }));
  element.dispatchEvent(new Event('change', { bubbles: true }));
  
  return {
    details: `Typed "${command.text}" into ${command.selector}`,
    elementTag: element.tagName.toLowerCase(),
    inputType: element.type || 'text'
  };
}

async function executeGetContentCommand(command) {
  const content = getPageContent();
  
  return {
    details: 'Retrieved page content',
    ...content
  };
}

function findElement(selector) {
  try {
    // First try exact selector
    let element = document.querySelector(selector);
    if (element) return element;
    
    // Try common search input selectors if original fails
    if (selector.includes('search') || selector.includes('input')) {
      const searchSelectors = [
        'input[type="search"]',
        'input[name="q"]',
        'input[name="query"]',
        'input[name="search"]',
        '#search',
        '#q',
        '.search-input',
        '[role="searchbox"]'
      ];
      
      for (const searchSelector of searchSelectors) {
        element = document.querySelector(searchSelector);
        if (element) {
          console.log(`Found element with fallback selector: ${searchSelector}`);
          return element;
        }
      }
    }
    
    return null;
  } catch (error) {
    console.error('Invalid selector:', selector, error);
    return null;
  }
}

function getPageContent() {
  return {
    html: document.body.innerHTML,
    title: document.title,
    url: window.location.href,
    text: document.body.innerText?.substring(0, 5000) || '', // Limit text size
    readyState: document.readyState
  };
}

async function waitForElementReady(element, timeout = 5000) {
  const startTime = Date.now();
  
  while (Date.now() - startTime < timeout) {
    if (isElementReady(element)) {
      return true;
    }
    await sleep(100);
  }
  
  console.warn('Element readiness timeout, proceeding anyway');
  return false;
}

function isElementReady(element) {
  const rect = element.getBoundingClientRect();
  return (
    element.offsetParent !== null && // Element is visible
    rect.width > 0 && rect.height > 0 && // Element has dimensions
    !element.disabled && // Element is not disabled
    window.getComputedStyle(element).display !== 'none' // Element is not hidden
  );
}

async function typeText(element, text, delay = 50) {
  for (let i = 0; i < text.length; i++) {
    const char = text[i];
    
    // Set value for input elements
    if (element.value !== undefined) {
      element.value += char;
    } else {
      element.textContent += char;
    }
    
    // Dispatch key events
    element.dispatchEvent(new KeyboardEvent('keydown', { key: char, bubbles: true }));
    element.dispatchEvent(new KeyboardEvent('keypress', { key: char, bubbles: true }));
    element.dispatchEvent(new KeyboardEvent('keyup', { key: char, bubbles: true }));
    
    await sleep(delay);
  }
}

function sleep(ms) {
  return new Promise(resolve => setTimeout(resolve, ms));
}

// Wait for page to be fully loaded before reporting ready
if (document.readyState === 'loading') {
  document.addEventListener('DOMContentLoaded', () => {
    console.log('Content script ready on:', window.location.href);
  });
} else {
  console.log('Content script ready on:', window.location.href);
}

// Export for debugging
window.cortexDebug = {
  executeCommand,
  findElement,
  getPageContent,
  typeText,
  sleep
};
