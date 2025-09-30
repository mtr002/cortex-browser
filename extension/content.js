console.log('Content script loaded on:', window.location.href);

// Listen for messages from background script
chrome.runtime.onMessage.addListener((message, sender, sendResponse) => {
  console.log('Content script received message:', message);
  
  // TODO: In future phases, this will execute commands like:
  // - Click elements
  // - Input text
  // - Navigate pages
  // - Get page content
  
  sendResponse({status: 'received'});
});

// Function to get page content (for future phases)
function getPageContent() {
  return {
    html: document.body.innerHTML,
    title: document.title,
    url: window.location.href
  };
}

// Function to find elements by selector (for future phases)
function findElement(selector) {
  try {
    return document.querySelector(selector);
  } catch (error) {
    console.error('Invalid selector:', selector, error);
    return null;
  }
}
