import React from 'react';
import './.config/jest-setup';
import { MessageChannel } from 'worker_threads';

global.React = React;

if (!global.MessageChannel) {
  global.MessageChannel = MessageChannel;
}

const mockIntersectionObserver = jest.fn().mockImplementation((callback) => ({
  observe: jest.fn().mockImplementation((elem) => {
    callback([{ target: elem, isIntersecting: true }]);
  }),
  unobserve: jest.fn(),
  disconnect: jest.fn(),
}));
global.IntersectionObserver = mockIntersectionObserver;

global.ResizeObserver = jest.fn().mockImplementation(() => ({
  observe: jest.fn(),
  unobserve: jest.fn(),
  disconnect: jest.fn(),
}));

Object.defineProperty(document, 'fonts', {
  value: {
    ready: Promise.resolve(),
    load: jest.fn(() => Promise.resolve([])),
    check: jest.fn(() => true),
  },
  writable: true,
});

HTMLCanvasElement.prototype.getContext = jest.fn(() => ({
  measureText: jest.fn(() => ({ width: 0 })),
  fillText: jest.fn(),
  clearRect: jest.fn(),
  fillRect: jest.fn(),
  beginPath: jest.fn(),
  moveTo: jest.fn(),
  lineTo: jest.fn(),
  stroke: jest.fn(),
  save: jest.fn(),
  restore: jest.fn(),
  scale: jest.fn(),
  translate: jest.fn(),
  drawImage: jest.fn(),
}));

const originalGetComputedStyle = window.getComputedStyle;
window.getComputedStyle = (elt, pseudoElt) => {
  const result = originalGetComputedStyle(elt, pseudoElt);
  if (!result || typeof result.getPropertyValue !== 'function') {
    return {
      getPropertyValue: () => '',
      ...result,
    };
  }
  return result;
};
