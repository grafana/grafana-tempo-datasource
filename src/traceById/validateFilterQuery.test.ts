import { validateFilterQuery } from './validateFilterQuery';

describe('validateFilterQuery', () => {
  it.each([
    '',
    '{ status = error }',
    '{ status = error && name = "foo" }',
    '{ name = "auth" || event.exception.message = "boom" }',
    '{ (event.exception.message = "boom" && event.level = "error") || name = "auth" }',
    '{}',
    '{ name = "x" }',
    '{ duration > 1 }',
    '{ span:id = "x" }',
    '{ event:name = "x" }',
    '{ resource.service.name = "x" }',
    '{ span.http.status_code = 200 }',
    '{ event.exception.message = "x" }',
    '{ link.foo = "x" }',
    '{ instrumentation.name = "x" }',
    '{ .foo = "x" }',
    '{ status = error && resource.service.name = "x" }',
    '{ span:duration > 1 }',
    '{ span:name = "x" }',
    '{ span:status = ok }',
    '{ span:statusMessage = "x" }',
    '{ span:kind = server }',
    '{ span:parentID = "x" }',
  ])('accepts valid query: %s', (query) => {
    expect(validateFilterQuery(query)).toBeUndefined();
  });

  it.each([
    '{ status = error } | count() > 2',
    '{ status = error } && { name = "foo" }',
    '{ status = error } >> { name = "foo" }',
    '{ status = error',
    'not even traceql',
    'resource.service.name = "some service"',
    '{ trace.foo = "x" }',
    '{ bogusscope.foo = "x" }',
    '{ status = error && trace.foo = "x" }',
    '{ parent.foo = "x" }',
    '{ parent = 1 }',
    '{ traceDuration > 1 }',
    '{ rootServiceName = "x" }',
    '{ rootName = "x" }',
    '{ parent:id = "x" }',
    '{ span:childCount > 0 }',
    '{ nestedSetLeft > 0 }',
    '{ trace:rootName = "foo" }',
    '{ trace:duration > 1s }',
    '{ trace:id = "abcd" }',
  ])('rejects invalid query: %s', (query) => {
    expect(validateFilterQuery(query)).toEqual(expect.any(String));
  });
});
