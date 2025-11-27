console.log('Content script loaded on:', window.location.href);

// Listen for messages from background script
chrome.runtime.onMessage.addListener((message, sender, sendResponse) => {
  console.log('Content script received message:', message);
  
  if (message.type === 'PING') {
    sendResponse({ status: 'ready' });
    return;
  }
  
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
  console.log('Current URL:', window.location.href);
  console.log('Document ready state:', document.readyState);
  
  // Ensure document is ready
  if (document.readyState === 'loading') {
    await new Promise(resolve => {
      document.addEventListener('DOMContentLoaded', resolve);
    });
  }
  
  try {
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
  } catch (error) {
    console.error('Command execution error:', error);
    console.error('Command details:', command);
    throw error;
  }
}

async function executeClickCommand(command) {
  if (!command.selector) {
    throw new Error('Click command requires selector');
  }

  // Special handling for search button selectors - try multiple strategies
  if (command.selector.includes('Search') || command.selector.includes('submit') || command.selector.includes('btn')) {
    const element = findSearchButton(command.selector);
    if (element) {
      await waitForElementReady(element);
      element.scrollIntoView({ behavior: 'smooth', block: 'center' });
      await sleep(500);
      element.click();
      return {
        details: `Clicked search button: ${element.tagName} ${element.name || element.value || element.textContent?.substring(0, 20)}`,
        elementText: element.textContent?.trim().substring(0, 50) || element.value || '',
        elementTag: element.tagName.toLowerCase()
      };
    }
  }

  const element = findElement(command.selector);
  if (!element) {
    // If it's a search button and we can't find it, try pressing Enter on the search input as fallback
    if (command.selector.includes('Search') || command.selector.includes('submit')) {
      const searchInput = document.querySelector('input[name="q"], textarea[name="q"], input[type="search"]');
      if (searchInput) {
        console.log('Search button not found, pressing Enter on search input instead');
        searchInput.focus();
        searchInput.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', code: 'Enter', keyCode: 13, bubbles: true }));
        searchInput.dispatchEvent(new KeyboardEvent('keyup', { key: 'Enter', code: 'Enter', keyCode: 13, bubbles: true }));
        return {
          details: 'Pressed Enter on search input (button not found)',
          elementText: '',
          elementTag: 'keyboard'
        };
      }
    }
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
    elementText: element.textContent?.trim().substring(0, 50) || element.value || '',
    elementTag: element.tagName.toLowerCase()
  };
}

// Find search button with multiple fallback strategies
function findSearchButton(selector) {
  // Try comma-separated selectors
  if (selector.includes(',')) {
    const selectors = selector.split(',').map(s => s.trim());
    for (const sel of selectors) {
      const element = document.querySelector(sel);
      if (element && isElementInteractable(element)) {
        return element;
      }
    }
  }
  
  // Try exact selector
  let element = document.querySelector(selector);
  if (element && isElementInteractable(element)) {
    return element;
  }
  
  // Google-specific search button selectors
  const googleSelectors = [
    'input[type="submit"][name="btnK"]',  // Google Search button
    'input[type="submit"][name="btnG"]',  // Google Search button (alternative)
    'input[value="Google Search"]',
    'input[value="Search"]',
    'button[type="submit"]',
    'input[type="submit"]',
    '[aria-label*="Search" i]',
    '[aria-label="Search"]',
    'button[name="btnK"]',
    'button[name="btnG"]'
  ];
  
  for (const sel of googleSelectors) {
    element = document.querySelector(sel);
    if (element && isElementInteractable(element)) {
      console.log(`Found search button with selector: ${sel}`);
      return element;
    }
  }
  
  // Try to find submit button in the same form as search input
  const searchInput = document.querySelector('input[name="q"], textarea[name="q"], input[type="search"]');
  if (searchInput && searchInput.form) {
    const submitButton = searchInput.form.querySelector('input[type="submit"], button[type="submit"]');
    if (submitButton && isElementInteractable(submitButton)) {
      console.log('Found submit button in search form');
      return submitButton;
    }
  }
  
  return null;
}

async function executeInputCommand(command) {
  console.log('Executing input command with selector:', command.selector);
  console.log('Input text:', command.text);
  
  if (!command.selector || !command.text) {
    throw new Error('Input command requires selector and text');
  }

  // Debug: Show available input elements
  const allInputs = document.querySelectorAll('input, textarea, [contenteditable]');
  console.log('Available input elements:', allInputs.length);
  allInputs.forEach((input, i) => {
    console.log(`Input ${i}:`, {
      tag: input.tagName,
      type: input.type || 'none',
      id: input.id || 'none',
      name: input.name || 'none',
      placeholder: input.placeholder || 'none',
      className: input.className || 'none',
      visible: isElementInteractable(input)
    });
  });

  const element = findElement(command.selector);
  if (!element) {
    throw new Error(`Input element not found with selector: ${command.selector}. Found ${allInputs.length} total input elements.`);
  }
  
  console.log('Found input element:', {
    tag: element.tagName,
    type: element.type,
    id: element.id,
    name: element.name
  });

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
  
  // Send content to backend for analysis if requested
  if (command.analyze) {
    // This would be handled by the background script
    chrome.runtime.sendMessage({
      type: 'PAGE_CONTENT',
      payload: content
    });
  }
  
  return {
    details: 'Retrieved page content',
    elementsFound: {
      inputs: document.querySelectorAll('input').length,
      buttons: document.querySelectorAll('button').length,
      links: document.querySelectorAll('a[href]').length,
      forms: document.querySelectorAll('form').length
    },
    ...content
  };
}

function findElement(selector) {
  try {
    // Handle comma-separated selectors (try each one individually)
    if (selector.includes(',')) {
      const selectors = selector.split(',').map(s => s.trim());
      console.log('Trying multiple selectors:', selectors);
      
      for (const sel of selectors) {
        const element = document.querySelector(sel);
        if (element && isElementInteractable(element)) {
          console.log(`Found element with selector: ${sel}`);
          return element;
        }
      }
    }
    
    // First try exact selector
    let element = document.querySelector(selector);
    if (element && isElementInteractable(element)) {
      return element;
    }
    
    // Try common search input selectors if original fails
    if (selector.includes('search') || selector.includes('input') || selector.includes('q')) {
      const searchSelectors = [
        'input[name="q"]',  // Google's main search box
        'textarea[name="q"]',  // Google sometimes uses textarea
        'input[type="search"]',
        'input[type="text"][name="q"]',
        'textarea[type="text"][name="q"]',
        'input[name="query"]',
        'input[name="search"]',
        '#search',
        '#q',
        '.search-input',
        '[role="searchbox"]',
        'input[placeholder*="search" i]',
        'input[placeholder*="Search" i]',
        'textarea[placeholder*="search" i]',
        'textarea[placeholder*="Search" i]',
        'input[aria-label*="Search" i]',
        'textarea[aria-label*="Search" i]'
      ];
      
      for (const searchSelector of searchSelectors) {
        element = document.querySelector(searchSelector);
        if (element && isElementInteractable(element)) {
          console.log(`Found element with fallback selector: ${searchSelector}`);
          return element;
        }
      }
      
      // Last resort: find any visible text input that might be a search box
      const allInputs = document.querySelectorAll('input[type="text"], input[type="search"], textarea');
      for (const input of allInputs) {
        if (isElementInteractable(input)) {
          const placeholder = (input.placeholder || '').toLowerCase();
          const name = (input.name || '').toLowerCase();
          const id = (input.id || '').toLowerCase();
          
          if (placeholder.includes('search') || name.includes('search') || name === 'q' || id.includes('search')) {
            console.log(`Found search input by content analysis: ${input.tagName} ${input.name || input.id || input.placeholder}`);
            return input;
          }
        }
      }
    }
    
    // Try button fallbacks
    if (selector.includes('button') || selector.includes('click')) {
      const buttonSelectors = [
        'button',
        'input[type="submit"]',
        'input[type="button"]',
        '[role="button"]',
        'a[href]'
      ];
      
      for (const btnSelector of buttonSelectors) {
        element = document.querySelector(btnSelector);
        if (element && isElementInteractable(element)) {
          console.log(`Found element with button fallback: ${btnSelector}`);
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

// Check if element is actually interactable
function isElementInteractable(element) {
  if (!element) return false;
  
  const rect = element.getBoundingClientRect();
  const style = window.getComputedStyle(element);
  
  return (
    rect.width > 0 &&
    rect.height > 0 &&
    style.display !== 'none' &&
    style.visibility !== 'hidden' &&
    !element.disabled &&
    element.offsetParent !== null
  );
}

function getPageContent() {
  // Get interactive elements for better analysis
  const interactiveElements = [];
  
  // Find all potentially interactive elements
  document.querySelectorAll('input, button, a[href], select, textarea').forEach((el, index) => {
    const rect = el.getBoundingClientRect();
    const isVisible = rect.width > 0 && rect.height > 0 && 
                     window.getComputedStyle(el).display !== 'none';
    
    if (isVisible) {
      interactiveElements.push({
        tag: el.tagName.toLowerCase(),
        type: el.type || '',
        id: el.id || '',
        name: el.name || '',
        className: el.className || '',
        text: el.textContent?.trim().substring(0, 100) || '',
        href: el.href || '',
        selector: generateElementSelector(el)
      });
    }
  });
  
  return {
    html: document.documentElement.outerHTML,
    title: document.title,
    url: window.location.href,
    text: document.body.innerText?.substring(0, 5000) || '',
    readyState: document.readyState,
    interactiveElements: interactiveElements,
    metadata: {
      hasSearchBox: !!document.querySelector('input[type="search"], input[name="q"], [role="searchbox"]'),
      hasForms: document.querySelectorAll('form').length > 0,
      hasNavigation: !!document.querySelector('nav, .navigation, .navbar'),
      domain: window.location.hostname
    }
  };
}

// Generate a reliable selector for an element
function generateElementSelector(element) {
  // Try ID first
  if (element.id) {
    return '#' + element.id;
  }
  
  // Try name attribute
  if (element.name) {
    return `[name="${element.name}"]`;
  }
  
  // Try role attribute
  if (element.getAttribute('role')) {
    return `[role="${element.getAttribute('role')}"]`;
  }
  
  // Try unique class
  if (element.className && typeof element.className === 'string') {
    const classes = element.className.split(' ').filter(c => c.length > 0);
    if (classes.length === 1) {
      return '.' + classes[0];
    }
  }
  
  // Try type for inputs
  if (element.tagName.toLowerCase() === 'input' && element.type) {
    return `input[type="${element.type}"]`;
  }
  
  // Fall back to tag name with position
  const siblings = Array.from(element.parentNode.children).filter(el => el.tagName === element.tagName);
  if (siblings.length === 1) {
    return element.tagName.toLowerCase();
  } else {
    const index = siblings.indexOf(element) + 1;
    return `${element.tagName.toLowerCase()}:nth-of-type(${index})`;
  }
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
  generateElementSelector,
  isElementInteractable,
  typeText,
  sleep
};
