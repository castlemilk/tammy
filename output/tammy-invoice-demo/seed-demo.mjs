var __create = Object.create;
var __defProp = Object.defineProperty;
var __getOwnPropDesc = Object.getOwnPropertyDescriptor;
var __getOwnPropNames = Object.getOwnPropertyNames;
var __getProtoOf = Object.getPrototypeOf;
var __hasOwnProp = Object.prototype.hasOwnProperty;
var __commonJS = (cb, mod) => function __require() {
  return mod || (0, cb[__getOwnPropNames(cb)[0]])((mod = { exports: {} }).exports, mod), mod.exports;
};
var __copyProps = (to, from, except, desc) => {
  if (from && typeof from === "object" || typeof from === "function") {
    for (let key of __getOwnPropNames(from))
      if (!__hasOwnProp.call(to, key) && key !== except)
        __defProp(to, key, { get: () => from[key], enumerable: !(desc = __getOwnPropDesc(from, key)) || desc.enumerable });
  }
  return to;
};
var __toESM = (mod, isNodeMode, target) => (target = mod != null ? __create(__getProtoOf(mod)) : {}, __copyProps(
  // If the importer is in node compatibility mode or this is not an ESM
  // file that has been converted to a CommonJS file using a Babel-
  // compatible transform (i.e. "__esModule" has not been set), then set
  // "default" to the CommonJS "module.exports" for node compatibility.
  isNodeMode || !mod || !mod.__esModule ? __defProp(target, "default", { value: mod, enumerable: true }) : target,
  mod
));

// node_modules/.pnpm/react@19.2.7/node_modules/react/cjs/react.production.js
var require_react_production = __commonJS({
  "node_modules/.pnpm/react@19.2.7/node_modules/react/cjs/react.production.js"(exports) {
    "use strict";
    var REACT_ELEMENT_TYPE = Symbol.for("react.transitional.element");
    var REACT_PORTAL_TYPE = Symbol.for("react.portal");
    var REACT_FRAGMENT_TYPE = Symbol.for("react.fragment");
    var REACT_STRICT_MODE_TYPE = Symbol.for("react.strict_mode");
    var REACT_PROFILER_TYPE = Symbol.for("react.profiler");
    var REACT_CONSUMER_TYPE = Symbol.for("react.consumer");
    var REACT_CONTEXT_TYPE = Symbol.for("react.context");
    var REACT_FORWARD_REF_TYPE = Symbol.for("react.forward_ref");
    var REACT_SUSPENSE_TYPE = Symbol.for("react.suspense");
    var REACT_MEMO_TYPE = Symbol.for("react.memo");
    var REACT_LAZY_TYPE = Symbol.for("react.lazy");
    var REACT_ACTIVITY_TYPE = Symbol.for("react.activity");
    var MAYBE_ITERATOR_SYMBOL = Symbol.iterator;
    function getIteratorFn(maybeIterable) {
      if (null === maybeIterable || "object" !== typeof maybeIterable) return null;
      maybeIterable = MAYBE_ITERATOR_SYMBOL && maybeIterable[MAYBE_ITERATOR_SYMBOL] || maybeIterable["@@iterator"];
      return "function" === typeof maybeIterable ? maybeIterable : null;
    }
    var ReactNoopUpdateQueue = {
      isMounted: function() {
        return false;
      },
      enqueueForceUpdate: function() {
      },
      enqueueReplaceState: function() {
      },
      enqueueSetState: function() {
      }
    };
    var assign = Object.assign;
    var emptyObject = {};
    function Component(props, context, updater) {
      this.props = props;
      this.context = context;
      this.refs = emptyObject;
      this.updater = updater || ReactNoopUpdateQueue;
    }
    Component.prototype.isReactComponent = {};
    Component.prototype.setState = function(partialState, callback) {
      if ("object" !== typeof partialState && "function" !== typeof partialState && null != partialState)
        throw Error(
          "takes an object of state variables to update or a function which returns an object of state variables."
        );
      this.updater.enqueueSetState(this, partialState, callback, "setState");
    };
    Component.prototype.forceUpdate = function(callback) {
      this.updater.enqueueForceUpdate(this, callback, "forceUpdate");
    };
    function ComponentDummy() {
    }
    ComponentDummy.prototype = Component.prototype;
    function PureComponent(props, context, updater) {
      this.props = props;
      this.context = context;
      this.refs = emptyObject;
      this.updater = updater || ReactNoopUpdateQueue;
    }
    var pureComponentPrototype = PureComponent.prototype = new ComponentDummy();
    pureComponentPrototype.constructor = PureComponent;
    assign(pureComponentPrototype, Component.prototype);
    pureComponentPrototype.isPureReactComponent = true;
    var isArrayImpl = Array.isArray;
    function noop() {
    }
    var ReactSharedInternals = { H: null, A: null, T: null, S: null };
    var hasOwnProperty = Object.prototype.hasOwnProperty;
    function ReactElement(type, key, props) {
      var refProp = props.ref;
      return {
        $$typeof: REACT_ELEMENT_TYPE,
        type,
        key,
        ref: void 0 !== refProp ? refProp : null,
        props
      };
    }
    function cloneAndReplaceKey(oldElement, newKey) {
      return ReactElement(oldElement.type, newKey, oldElement.props);
    }
    function isValidElement2(object2) {
      return "object" === typeof object2 && null !== object2 && object2.$$typeof === REACT_ELEMENT_TYPE;
    }
    function escape(key) {
      var escaperLookup = { "=": "=0", ":": "=2" };
      return "$" + key.replace(/[=:]/g, function(match) {
        return escaperLookup[match];
      });
    }
    var userProvidedKeyEscapeRegex = /\/+/g;
    function getElementKey(element, index) {
      return "object" === typeof element && null !== element && null != element.key ? escape("" + element.key) : index.toString(36);
    }
    function resolveThenable(thenable) {
      switch (thenable.status) {
        case "fulfilled":
          return thenable.value;
        case "rejected":
          throw thenable.reason;
        default:
          switch ("string" === typeof thenable.status ? thenable.then(noop, noop) : (thenable.status = "pending", thenable.then(
            function(fulfilledValue) {
              "pending" === thenable.status && (thenable.status = "fulfilled", thenable.value = fulfilledValue);
            },
            function(error) {
              "pending" === thenable.status && (thenable.status = "rejected", thenable.reason = error);
            }
          )), thenable.status) {
            case "fulfilled":
              return thenable.value;
            case "rejected":
              throw thenable.reason;
          }
      }
      throw thenable;
    }
    function mapIntoArray(children, array, escapedPrefix, nameSoFar, callback) {
      var type = typeof children;
      if ("undefined" === type || "boolean" === type) children = null;
      var invokeCallback = false;
      if (null === children) invokeCallback = true;
      else
        switch (type) {
          case "bigint":
          case "string":
          case "number":
            invokeCallback = true;
            break;
          case "object":
            switch (children.$$typeof) {
              case REACT_ELEMENT_TYPE:
              case REACT_PORTAL_TYPE:
                invokeCallback = true;
                break;
              case REACT_LAZY_TYPE:
                return invokeCallback = children._init, mapIntoArray(
                  invokeCallback(children._payload),
                  array,
                  escapedPrefix,
                  nameSoFar,
                  callback
                );
            }
        }
      if (invokeCallback)
        return callback = callback(children), invokeCallback = "" === nameSoFar ? "." + getElementKey(children, 0) : nameSoFar, isArrayImpl(callback) ? (escapedPrefix = "", null != invokeCallback && (escapedPrefix = invokeCallback.replace(userProvidedKeyEscapeRegex, "$&/") + "/"), mapIntoArray(callback, array, escapedPrefix, "", function(c) {
          return c;
        })) : null != callback && (isValidElement2(callback) && (callback = cloneAndReplaceKey(
          callback,
          escapedPrefix + (null == callback.key || children && children.key === callback.key ? "" : ("" + callback.key).replace(
            userProvidedKeyEscapeRegex,
            "$&/"
          ) + "/") + invokeCallback
        )), array.push(callback)), 1;
      invokeCallback = 0;
      var nextNamePrefix = "" === nameSoFar ? "." : nameSoFar + ":";
      if (isArrayImpl(children))
        for (var i = 0; i < children.length; i++)
          nameSoFar = children[i], type = nextNamePrefix + getElementKey(nameSoFar, i), invokeCallback += mapIntoArray(
            nameSoFar,
            array,
            escapedPrefix,
            type,
            callback
          );
      else if (i = getIteratorFn(children), "function" === typeof i)
        for (children = i.call(children), i = 0; !(nameSoFar = children.next()).done; )
          nameSoFar = nameSoFar.value, type = nextNamePrefix + getElementKey(nameSoFar, i++), invokeCallback += mapIntoArray(
            nameSoFar,
            array,
            escapedPrefix,
            type,
            callback
          );
      else if ("object" === type) {
        if ("function" === typeof children.then)
          return mapIntoArray(
            resolveThenable(children),
            array,
            escapedPrefix,
            nameSoFar,
            callback
          );
        array = String(children);
        throw Error(
          "Objects are not valid as a React child (found: " + ("[object Object]" === array ? "object with keys {" + Object.keys(children).join(", ") + "}" : array) + "). If you meant to render a collection of children, use an array instead."
        );
      }
      return invokeCallback;
    }
    function mapChildren(children, func, context) {
      if (null == children) return children;
      var result = [], count = 0;
      mapIntoArray(children, result, "", "", function(child) {
        return func.call(context, child, count++);
      });
      return result;
    }
    function lazyInitializer(payload) {
      if (-1 === payload._status) {
        var ctor = payload._result;
        ctor = ctor();
        ctor.then(
          function(moduleObject) {
            if (0 === payload._status || -1 === payload._status)
              payload._status = 1, payload._result = moduleObject;
          },
          function(error) {
            if (0 === payload._status || -1 === payload._status)
              payload._status = 2, payload._result = error;
          }
        );
        -1 === payload._status && (payload._status = 0, payload._result = ctor);
      }
      if (1 === payload._status) return payload._result.default;
      throw payload._result;
    }
    var reportGlobalError = "function" === typeof reportError ? reportError : function(error) {
      if ("object" === typeof window && "function" === typeof window.ErrorEvent) {
        var event = new window.ErrorEvent("error", {
          bubbles: true,
          cancelable: true,
          message: "object" === typeof error && null !== error && "string" === typeof error.message ? String(error.message) : String(error),
          error
        });
        if (!window.dispatchEvent(event)) return;
      } else if ("object" === typeof process && "function" === typeof process.emit) {
        process.emit("uncaughtException", error);
        return;
      }
      console.error(error);
    };
    var Children2 = {
      map: mapChildren,
      forEach: function(children, forEachFunc, forEachContext) {
        mapChildren(
          children,
          function() {
            forEachFunc.apply(this, arguments);
          },
          forEachContext
        );
      },
      count: function(children) {
        var n = 0;
        mapChildren(children, function() {
          n++;
        });
        return n;
      },
      toArray: function(children) {
        return mapChildren(children, function(child) {
          return child;
        }) || [];
      },
      only: function(children) {
        if (!isValidElement2(children))
          throw Error(
            "React.Children.only expected to receive a single React element child."
          );
        return children;
      }
    };
    exports.Activity = REACT_ACTIVITY_TYPE;
    exports.Children = Children2;
    exports.Component = Component;
    exports.Fragment = REACT_FRAGMENT_TYPE;
    exports.Profiler = REACT_PROFILER_TYPE;
    exports.PureComponent = PureComponent;
    exports.StrictMode = REACT_STRICT_MODE_TYPE;
    exports.Suspense = REACT_SUSPENSE_TYPE;
    exports.__CLIENT_INTERNALS_DO_NOT_USE_OR_WARN_USERS_THEY_CANNOT_UPGRADE = ReactSharedInternals;
    exports.__COMPILER_RUNTIME = {
      __proto__: null,
      c: function(size) {
        return ReactSharedInternals.H.useMemoCache(size);
      }
    };
    exports.cache = function(fn) {
      return function() {
        return fn.apply(null, arguments);
      };
    };
    exports.cacheSignal = function() {
      return null;
    };
    exports.cloneElement = function(element, config, children) {
      if (null === element || void 0 === element)
        throw Error(
          "The argument must be a React element, but you passed " + element + "."
        );
      var props = assign({}, element.props), key = element.key;
      if (null != config)
        for (propName in void 0 !== config.key && (key = "" + config.key), config)
          !hasOwnProperty.call(config, propName) || "key" === propName || "__self" === propName || "__source" === propName || "ref" === propName && void 0 === config.ref || (props[propName] = config[propName]);
      var propName = arguments.length - 2;
      if (1 === propName) props.children = children;
      else if (1 < propName) {
        for (var childArray = Array(propName), i = 0; i < propName; i++)
          childArray[i] = arguments[i + 2];
        props.children = childArray;
      }
      return ReactElement(element.type, key, props);
    };
    exports.createContext = function(defaultValue) {
      defaultValue = {
        $$typeof: REACT_CONTEXT_TYPE,
        _currentValue: defaultValue,
        _currentValue2: defaultValue,
        _threadCount: 0,
        Provider: null,
        Consumer: null
      };
      defaultValue.Provider = defaultValue;
      defaultValue.Consumer = {
        $$typeof: REACT_CONSUMER_TYPE,
        _context: defaultValue
      };
      return defaultValue;
    };
    exports.createElement = function(type, config, children) {
      var propName, props = {}, key = null;
      if (null != config)
        for (propName in void 0 !== config.key && (key = "" + config.key), config)
          hasOwnProperty.call(config, propName) && "key" !== propName && "__self" !== propName && "__source" !== propName && (props[propName] = config[propName]);
      var childrenLength = arguments.length - 2;
      if (1 === childrenLength) props.children = children;
      else if (1 < childrenLength) {
        for (var childArray = Array(childrenLength), i = 0; i < childrenLength; i++)
          childArray[i] = arguments[i + 2];
        props.children = childArray;
      }
      if (type && type.defaultProps)
        for (propName in childrenLength = type.defaultProps, childrenLength)
          void 0 === props[propName] && (props[propName] = childrenLength[propName]);
      return ReactElement(type, key, props);
    };
    exports.createRef = function() {
      return { current: null };
    };
    exports.forwardRef = function(render2) {
      return { $$typeof: REACT_FORWARD_REF_TYPE, render: render2 };
    };
    exports.isValidElement = isValidElement2;
    exports.lazy = function(ctor) {
      return {
        $$typeof: REACT_LAZY_TYPE,
        _payload: { _status: -1, _result: ctor },
        _init: lazyInitializer
      };
    };
    exports.memo = function(type, compare) {
      return {
        $$typeof: REACT_MEMO_TYPE,
        type,
        compare: void 0 === compare ? null : compare
      };
    };
    exports.startTransition = function(scope) {
      var prevTransition = ReactSharedInternals.T, currentTransition = {};
      ReactSharedInternals.T = currentTransition;
      try {
        var returnValue = scope(), onStartTransitionFinish = ReactSharedInternals.S;
        null !== onStartTransitionFinish && onStartTransitionFinish(currentTransition, returnValue);
        "object" === typeof returnValue && null !== returnValue && "function" === typeof returnValue.then && returnValue.then(noop, reportGlobalError);
      } catch (error) {
        reportGlobalError(error);
      } finally {
        null !== prevTransition && null !== currentTransition.types && (prevTransition.types = currentTransition.types), ReactSharedInternals.T = prevTransition;
      }
    };
    exports.unstable_useCacheRefresh = function() {
      return ReactSharedInternals.H.useCacheRefresh();
    };
    exports.use = function(usable) {
      return ReactSharedInternals.H.use(usable);
    };
    exports.useActionState = function(action, initialState, permalink) {
      return ReactSharedInternals.H.useActionState(action, initialState, permalink);
    };
    exports.useCallback = function(callback, deps) {
      return ReactSharedInternals.H.useCallback(callback, deps);
    };
    exports.useContext = function(Context) {
      return ReactSharedInternals.H.useContext(Context);
    };
    exports.useDebugValue = function() {
    };
    exports.useDeferredValue = function(value, initialValue) {
      return ReactSharedInternals.H.useDeferredValue(value, initialValue);
    };
    exports.useEffect = function(create2, deps) {
      return ReactSharedInternals.H.useEffect(create2, deps);
    };
    exports.useEffectEvent = function(callback) {
      return ReactSharedInternals.H.useEffectEvent(callback);
    };
    exports.useId = function() {
      return ReactSharedInternals.H.useId();
    };
    exports.useImperativeHandle = function(ref, create2, deps) {
      return ReactSharedInternals.H.useImperativeHandle(ref, create2, deps);
    };
    exports.useInsertionEffect = function(create2, deps) {
      return ReactSharedInternals.H.useInsertionEffect(create2, deps);
    };
    exports.useLayoutEffect = function(create2, deps) {
      return ReactSharedInternals.H.useLayoutEffect(create2, deps);
    };
    exports.useMemo = function(create2, deps) {
      return ReactSharedInternals.H.useMemo(create2, deps);
    };
    exports.useOptimistic = function(passthrough, reducer) {
      return ReactSharedInternals.H.useOptimistic(passthrough, reducer);
    };
    exports.useReducer = function(reducer, initialArg, init) {
      return ReactSharedInternals.H.useReducer(reducer, initialArg, init);
    };
    exports.useRef = function(initialValue) {
      return ReactSharedInternals.H.useRef(initialValue);
    };
    exports.useState = function(initialState) {
      return ReactSharedInternals.H.useState(initialState);
    };
    exports.useSyncExternalStore = function(subscribe, getSnapshot, getServerSnapshot) {
      return ReactSharedInternals.H.useSyncExternalStore(
        subscribe,
        getSnapshot,
        getServerSnapshot
      );
    };
    exports.useTransition = function() {
      return ReactSharedInternals.H.useTransition();
    };
    exports.version = "19.2.7";
  }
});

// node_modules/.pnpm/react@19.2.7/node_modules/react/cjs/react.development.js
var require_react_development = __commonJS({
  "node_modules/.pnpm/react@19.2.7/node_modules/react/cjs/react.development.js"(exports, module) {
    "use strict";
    "production" !== process.env.NODE_ENV && (function() {
      function defineDeprecationWarning(methodName, info) {
        Object.defineProperty(Component.prototype, methodName, {
          get: function() {
            console.warn(
              "%s(...) is deprecated in plain JavaScript React classes. %s",
              info[0],
              info[1]
            );
          }
        });
      }
      function getIteratorFn(maybeIterable) {
        if (null === maybeIterable || "object" !== typeof maybeIterable)
          return null;
        maybeIterable = MAYBE_ITERATOR_SYMBOL && maybeIterable[MAYBE_ITERATOR_SYMBOL] || maybeIterable["@@iterator"];
        return "function" === typeof maybeIterable ? maybeIterable : null;
      }
      function warnNoop(publicInstance, callerName) {
        publicInstance = (publicInstance = publicInstance.constructor) && (publicInstance.displayName || publicInstance.name) || "ReactClass";
        var warningKey = publicInstance + "." + callerName;
        didWarnStateUpdateForUnmountedComponent[warningKey] || (console.error(
          "Can't call %s on a component that is not yet mounted. This is a no-op, but it might indicate a bug in your application. Instead, assign to `this.state` directly or define a `state = {};` class property with the desired state in the %s component.",
          callerName,
          publicInstance
        ), didWarnStateUpdateForUnmountedComponent[warningKey] = true);
      }
      function Component(props, context, updater) {
        this.props = props;
        this.context = context;
        this.refs = emptyObject;
        this.updater = updater || ReactNoopUpdateQueue;
      }
      function ComponentDummy() {
      }
      function PureComponent(props, context, updater) {
        this.props = props;
        this.context = context;
        this.refs = emptyObject;
        this.updater = updater || ReactNoopUpdateQueue;
      }
      function noop() {
      }
      function testStringCoercion(value) {
        return "" + value;
      }
      function checkKeyStringCoercion(value) {
        try {
          testStringCoercion(value);
          var JSCompiler_inline_result = false;
        } catch (e) {
          JSCompiler_inline_result = true;
        }
        if (JSCompiler_inline_result) {
          JSCompiler_inline_result = console;
          var JSCompiler_temp_const = JSCompiler_inline_result.error;
          var JSCompiler_inline_result$jscomp$0 = "function" === typeof Symbol && Symbol.toStringTag && value[Symbol.toStringTag] || value.constructor.name || "Object";
          JSCompiler_temp_const.call(
            JSCompiler_inline_result,
            "The provided key is an unsupported type %s. This value must be coerced to a string before using it here.",
            JSCompiler_inline_result$jscomp$0
          );
          return testStringCoercion(value);
        }
      }
      function getComponentNameFromType(type) {
        if (null == type) return null;
        if ("function" === typeof type)
          return type.$$typeof === REACT_CLIENT_REFERENCE ? null : type.displayName || type.name || null;
        if ("string" === typeof type) return type;
        switch (type) {
          case REACT_FRAGMENT_TYPE:
            return "Fragment";
          case REACT_PROFILER_TYPE:
            return "Profiler";
          case REACT_STRICT_MODE_TYPE:
            return "StrictMode";
          case REACT_SUSPENSE_TYPE:
            return "Suspense";
          case REACT_SUSPENSE_LIST_TYPE:
            return "SuspenseList";
          case REACT_ACTIVITY_TYPE:
            return "Activity";
        }
        if ("object" === typeof type)
          switch ("number" === typeof type.tag && console.error(
            "Received an unexpected object in getComponentNameFromType(). This is likely a bug in React. Please file an issue."
          ), type.$$typeof) {
            case REACT_PORTAL_TYPE:
              return "Portal";
            case REACT_CONTEXT_TYPE:
              return type.displayName || "Context";
            case REACT_CONSUMER_TYPE:
              return (type._context.displayName || "Context") + ".Consumer";
            case REACT_FORWARD_REF_TYPE:
              var innerType = type.render;
              type = type.displayName;
              type || (type = innerType.displayName || innerType.name || "", type = "" !== type ? "ForwardRef(" + type + ")" : "ForwardRef");
              return type;
            case REACT_MEMO_TYPE:
              return innerType = type.displayName || null, null !== innerType ? innerType : getComponentNameFromType(type.type) || "Memo";
            case REACT_LAZY_TYPE:
              innerType = type._payload;
              type = type._init;
              try {
                return getComponentNameFromType(type(innerType));
              } catch (x) {
              }
          }
        return null;
      }
      function getTaskName(type) {
        if (type === REACT_FRAGMENT_TYPE) return "<>";
        if ("object" === typeof type && null !== type && type.$$typeof === REACT_LAZY_TYPE)
          return "<...>";
        try {
          var name = getComponentNameFromType(type);
          return name ? "<" + name + ">" : "<...>";
        } catch (x) {
          return "<...>";
        }
      }
      function getOwner() {
        var dispatcher = ReactSharedInternals.A;
        return null === dispatcher ? null : dispatcher.getOwner();
      }
      function UnknownOwner() {
        return Error("react-stack-top-frame");
      }
      function hasValidKey(config) {
        if (hasOwnProperty.call(config, "key")) {
          var getter = Object.getOwnPropertyDescriptor(config, "key").get;
          if (getter && getter.isReactWarning) return false;
        }
        return void 0 !== config.key;
      }
      function defineKeyPropWarningGetter(props, displayName) {
        function warnAboutAccessingKey() {
          specialPropKeyWarningShown || (specialPropKeyWarningShown = true, console.error(
            "%s: `key` is not a prop. Trying to access it will result in `undefined` being returned. If you need to access the same value within the child component, you should pass it as a different prop. (https://react.dev/link/special-props)",
            displayName
          ));
        }
        warnAboutAccessingKey.isReactWarning = true;
        Object.defineProperty(props, "key", {
          get: warnAboutAccessingKey,
          configurable: true
        });
      }
      function elementRefGetterWithDeprecationWarning() {
        var componentName = getComponentNameFromType(this.type);
        didWarnAboutElementRef[componentName] || (didWarnAboutElementRef[componentName] = true, console.error(
          "Accessing element.ref was removed in React 19. ref is now a regular prop. It will be removed from the JSX Element type in a future release."
        ));
        componentName = this.props.ref;
        return void 0 !== componentName ? componentName : null;
      }
      function ReactElement(type, key, props, owner, debugStack, debugTask) {
        var refProp = props.ref;
        type = {
          $$typeof: REACT_ELEMENT_TYPE,
          type,
          key,
          props,
          _owner: owner
        };
        null !== (void 0 !== refProp ? refProp : null) ? Object.defineProperty(type, "ref", {
          enumerable: false,
          get: elementRefGetterWithDeprecationWarning
        }) : Object.defineProperty(type, "ref", { enumerable: false, value: null });
        type._store = {};
        Object.defineProperty(type._store, "validated", {
          configurable: false,
          enumerable: false,
          writable: true,
          value: 0
        });
        Object.defineProperty(type, "_debugInfo", {
          configurable: false,
          enumerable: false,
          writable: true,
          value: null
        });
        Object.defineProperty(type, "_debugStack", {
          configurable: false,
          enumerable: false,
          writable: true,
          value: debugStack
        });
        Object.defineProperty(type, "_debugTask", {
          configurable: false,
          enumerable: false,
          writable: true,
          value: debugTask
        });
        Object.freeze && (Object.freeze(type.props), Object.freeze(type));
        return type;
      }
      function cloneAndReplaceKey(oldElement, newKey) {
        newKey = ReactElement(
          oldElement.type,
          newKey,
          oldElement.props,
          oldElement._owner,
          oldElement._debugStack,
          oldElement._debugTask
        );
        oldElement._store && (newKey._store.validated = oldElement._store.validated);
        return newKey;
      }
      function validateChildKeys(node) {
        isValidElement2(node) ? node._store && (node._store.validated = 1) : "object" === typeof node && null !== node && node.$$typeof === REACT_LAZY_TYPE && ("fulfilled" === node._payload.status ? isValidElement2(node._payload.value) && node._payload.value._store && (node._payload.value._store.validated = 1) : node._store && (node._store.validated = 1));
      }
      function isValidElement2(object2) {
        return "object" === typeof object2 && null !== object2 && object2.$$typeof === REACT_ELEMENT_TYPE;
      }
      function escape(key) {
        var escaperLookup = { "=": "=0", ":": "=2" };
        return "$" + key.replace(/[=:]/g, function(match) {
          return escaperLookup[match];
        });
      }
      function getElementKey(element, index) {
        return "object" === typeof element && null !== element && null != element.key ? (checkKeyStringCoercion(element.key), escape("" + element.key)) : index.toString(36);
      }
      function resolveThenable(thenable) {
        switch (thenable.status) {
          case "fulfilled":
            return thenable.value;
          case "rejected":
            throw thenable.reason;
          default:
            switch ("string" === typeof thenable.status ? thenable.then(noop, noop) : (thenable.status = "pending", thenable.then(
              function(fulfilledValue) {
                "pending" === thenable.status && (thenable.status = "fulfilled", thenable.value = fulfilledValue);
              },
              function(error) {
                "pending" === thenable.status && (thenable.status = "rejected", thenable.reason = error);
              }
            )), thenable.status) {
              case "fulfilled":
                return thenable.value;
              case "rejected":
                throw thenable.reason;
            }
        }
        throw thenable;
      }
      function mapIntoArray(children, array, escapedPrefix, nameSoFar, callback) {
        var type = typeof children;
        if ("undefined" === type || "boolean" === type) children = null;
        var invokeCallback = false;
        if (null === children) invokeCallback = true;
        else
          switch (type) {
            case "bigint":
            case "string":
            case "number":
              invokeCallback = true;
              break;
            case "object":
              switch (children.$$typeof) {
                case REACT_ELEMENT_TYPE:
                case REACT_PORTAL_TYPE:
                  invokeCallback = true;
                  break;
                case REACT_LAZY_TYPE:
                  return invokeCallback = children._init, mapIntoArray(
                    invokeCallback(children._payload),
                    array,
                    escapedPrefix,
                    nameSoFar,
                    callback
                  );
              }
          }
        if (invokeCallback) {
          invokeCallback = children;
          callback = callback(invokeCallback);
          var childKey = "" === nameSoFar ? "." + getElementKey(invokeCallback, 0) : nameSoFar;
          isArrayImpl(callback) ? (escapedPrefix = "", null != childKey && (escapedPrefix = childKey.replace(userProvidedKeyEscapeRegex, "$&/") + "/"), mapIntoArray(callback, array, escapedPrefix, "", function(c) {
            return c;
          })) : null != callback && (isValidElement2(callback) && (null != callback.key && (invokeCallback && invokeCallback.key === callback.key || checkKeyStringCoercion(callback.key)), escapedPrefix = cloneAndReplaceKey(
            callback,
            escapedPrefix + (null == callback.key || invokeCallback && invokeCallback.key === callback.key ? "" : ("" + callback.key).replace(
              userProvidedKeyEscapeRegex,
              "$&/"
            ) + "/") + childKey
          ), "" !== nameSoFar && null != invokeCallback && isValidElement2(invokeCallback) && null == invokeCallback.key && invokeCallback._store && !invokeCallback._store.validated && (escapedPrefix._store.validated = 2), callback = escapedPrefix), array.push(callback));
          return 1;
        }
        invokeCallback = 0;
        childKey = "" === nameSoFar ? "." : nameSoFar + ":";
        if (isArrayImpl(children))
          for (var i = 0; i < children.length; i++)
            nameSoFar = children[i], type = childKey + getElementKey(nameSoFar, i), invokeCallback += mapIntoArray(
              nameSoFar,
              array,
              escapedPrefix,
              type,
              callback
            );
        else if (i = getIteratorFn(children), "function" === typeof i)
          for (i === children.entries && (didWarnAboutMaps || console.warn(
            "Using Maps as children is not supported. Use an array of keyed ReactElements instead."
          ), didWarnAboutMaps = true), children = i.call(children), i = 0; !(nameSoFar = children.next()).done; )
            nameSoFar = nameSoFar.value, type = childKey + getElementKey(nameSoFar, i++), invokeCallback += mapIntoArray(
              nameSoFar,
              array,
              escapedPrefix,
              type,
              callback
            );
        else if ("object" === type) {
          if ("function" === typeof children.then)
            return mapIntoArray(
              resolveThenable(children),
              array,
              escapedPrefix,
              nameSoFar,
              callback
            );
          array = String(children);
          throw Error(
            "Objects are not valid as a React child (found: " + ("[object Object]" === array ? "object with keys {" + Object.keys(children).join(", ") + "}" : array) + "). If you meant to render a collection of children, use an array instead."
          );
        }
        return invokeCallback;
      }
      function mapChildren(children, func, context) {
        if (null == children) return children;
        var result = [], count = 0;
        mapIntoArray(children, result, "", "", function(child) {
          return func.call(context, child, count++);
        });
        return result;
      }
      function lazyInitializer(payload) {
        if (-1 === payload._status) {
          var ioInfo = payload._ioInfo;
          null != ioInfo && (ioInfo.start = ioInfo.end = performance.now());
          ioInfo = payload._result;
          var thenable = ioInfo();
          thenable.then(
            function(moduleObject) {
              if (0 === payload._status || -1 === payload._status) {
                payload._status = 1;
                payload._result = moduleObject;
                var _ioInfo = payload._ioInfo;
                null != _ioInfo && (_ioInfo.end = performance.now());
                void 0 === thenable.status && (thenable.status = "fulfilled", thenable.value = moduleObject);
              }
            },
            function(error) {
              if (0 === payload._status || -1 === payload._status) {
                payload._status = 2;
                payload._result = error;
                var _ioInfo2 = payload._ioInfo;
                null != _ioInfo2 && (_ioInfo2.end = performance.now());
                void 0 === thenable.status && (thenable.status = "rejected", thenable.reason = error);
              }
            }
          );
          ioInfo = payload._ioInfo;
          if (null != ioInfo) {
            ioInfo.value = thenable;
            var displayName = thenable.displayName;
            "string" === typeof displayName && (ioInfo.name = displayName);
          }
          -1 === payload._status && (payload._status = 0, payload._result = thenable);
        }
        if (1 === payload._status)
          return ioInfo = payload._result, void 0 === ioInfo && console.error(
            "lazy: Expected the result of a dynamic import() call. Instead received: %s\n\nYour code should look like: \n  const MyComponent = lazy(() => import('./MyComponent'))\n\nDid you accidentally put curly braces around the import?",
            ioInfo
          ), "default" in ioInfo || console.error(
            "lazy: Expected the result of a dynamic import() call. Instead received: %s\n\nYour code should look like: \n  const MyComponent = lazy(() => import('./MyComponent'))",
            ioInfo
          ), ioInfo.default;
        throw payload._result;
      }
      function resolveDispatcher() {
        var dispatcher = ReactSharedInternals.H;
        null === dispatcher && console.error(
          "Invalid hook call. Hooks can only be called inside of the body of a function component. This could happen for one of the following reasons:\n1. You might have mismatching versions of React and the renderer (such as React DOM)\n2. You might be breaking the Rules of Hooks\n3. You might have more than one copy of React in the same app\nSee https://react.dev/link/invalid-hook-call for tips about how to debug and fix this problem."
        );
        return dispatcher;
      }
      function releaseAsyncTransition() {
        ReactSharedInternals.asyncTransitions--;
      }
      function enqueueTask(task) {
        if (null === enqueueTaskImpl)
          try {
            var requireString = ("require" + Math.random()).slice(0, 7);
            enqueueTaskImpl = (module && module[requireString]).call(
              module,
              "timers"
            ).setImmediate;
          } catch (_err) {
            enqueueTaskImpl = function(callback) {
              false === didWarnAboutMessageChannel && (didWarnAboutMessageChannel = true, "undefined" === typeof MessageChannel && console.error(
                "This browser does not have a MessageChannel implementation, so enqueuing tasks via await act(async () => ...) will fail. Please file an issue at https://github.com/facebook/react/issues if you encounter this warning."
              ));
              var channel = new MessageChannel();
              channel.port1.onmessage = callback;
              channel.port2.postMessage(void 0);
            };
          }
        return enqueueTaskImpl(task);
      }
      function aggregateErrors(errors2) {
        return 1 < errors2.length && "function" === typeof AggregateError ? new AggregateError(errors2) : errors2[0];
      }
      function popActScope(prevActQueue, prevActScopeDepth) {
        prevActScopeDepth !== actScopeDepth - 1 && console.error(
          "You seem to have overlapping act() calls, this is not supported. Be sure to await previous act() calls before making a new one. "
        );
        actScopeDepth = prevActScopeDepth;
      }
      function recursivelyFlushAsyncActWork(returnValue, resolve, reject) {
        var queue = ReactSharedInternals.actQueue;
        if (null !== queue)
          if (0 !== queue.length)
            try {
              flushActQueue(queue);
              enqueueTask(function() {
                return recursivelyFlushAsyncActWork(returnValue, resolve, reject);
              });
              return;
            } catch (error) {
              ReactSharedInternals.thrownErrors.push(error);
            }
          else ReactSharedInternals.actQueue = null;
        0 < ReactSharedInternals.thrownErrors.length ? (queue = aggregateErrors(ReactSharedInternals.thrownErrors), ReactSharedInternals.thrownErrors.length = 0, reject(queue)) : resolve(returnValue);
      }
      function flushActQueue(queue) {
        if (!isFlushing) {
          isFlushing = true;
          var i = 0;
          try {
            for (; i < queue.length; i++) {
              var callback = queue[i];
              do {
                ReactSharedInternals.didUsePromise = false;
                var continuation = callback(false);
                if (null !== continuation) {
                  if (ReactSharedInternals.didUsePromise) {
                    queue[i] = callback;
                    queue.splice(0, i);
                    return;
                  }
                  callback = continuation;
                } else break;
              } while (1);
            }
            queue.length = 0;
          } catch (error) {
            queue.splice(0, i + 1), ReactSharedInternals.thrownErrors.push(error);
          } finally {
            isFlushing = false;
          }
        }
      }
      "undefined" !== typeof __REACT_DEVTOOLS_GLOBAL_HOOK__ && "function" === typeof __REACT_DEVTOOLS_GLOBAL_HOOK__.registerInternalModuleStart && __REACT_DEVTOOLS_GLOBAL_HOOK__.registerInternalModuleStart(Error());
      var REACT_ELEMENT_TYPE = Symbol.for("react.transitional.element"), REACT_PORTAL_TYPE = Symbol.for("react.portal"), REACT_FRAGMENT_TYPE = Symbol.for("react.fragment"), REACT_STRICT_MODE_TYPE = Symbol.for("react.strict_mode"), REACT_PROFILER_TYPE = Symbol.for("react.profiler"), REACT_CONSUMER_TYPE = Symbol.for("react.consumer"), REACT_CONTEXT_TYPE = Symbol.for("react.context"), REACT_FORWARD_REF_TYPE = Symbol.for("react.forward_ref"), REACT_SUSPENSE_TYPE = Symbol.for("react.suspense"), REACT_SUSPENSE_LIST_TYPE = Symbol.for("react.suspense_list"), REACT_MEMO_TYPE = Symbol.for("react.memo"), REACT_LAZY_TYPE = Symbol.for("react.lazy"), REACT_ACTIVITY_TYPE = Symbol.for("react.activity"), MAYBE_ITERATOR_SYMBOL = Symbol.iterator, didWarnStateUpdateForUnmountedComponent = {}, ReactNoopUpdateQueue = {
        isMounted: function() {
          return false;
        },
        enqueueForceUpdate: function(publicInstance) {
          warnNoop(publicInstance, "forceUpdate");
        },
        enqueueReplaceState: function(publicInstance) {
          warnNoop(publicInstance, "replaceState");
        },
        enqueueSetState: function(publicInstance) {
          warnNoop(publicInstance, "setState");
        }
      }, assign = Object.assign, emptyObject = {};
      Object.freeze(emptyObject);
      Component.prototype.isReactComponent = {};
      Component.prototype.setState = function(partialState, callback) {
        if ("object" !== typeof partialState && "function" !== typeof partialState && null != partialState)
          throw Error(
            "takes an object of state variables to update or a function which returns an object of state variables."
          );
        this.updater.enqueueSetState(this, partialState, callback, "setState");
      };
      Component.prototype.forceUpdate = function(callback) {
        this.updater.enqueueForceUpdate(this, callback, "forceUpdate");
      };
      var deprecatedAPIs = {
        isMounted: [
          "isMounted",
          "Instead, make sure to clean up subscriptions and pending requests in componentWillUnmount to prevent memory leaks."
        ],
        replaceState: [
          "replaceState",
          "Refactor your code to use setState instead (see https://github.com/facebook/react/issues/3236)."
        ]
      };
      for (fnName in deprecatedAPIs)
        deprecatedAPIs.hasOwnProperty(fnName) && defineDeprecationWarning(fnName, deprecatedAPIs[fnName]);
      ComponentDummy.prototype = Component.prototype;
      deprecatedAPIs = PureComponent.prototype = new ComponentDummy();
      deprecatedAPIs.constructor = PureComponent;
      assign(deprecatedAPIs, Component.prototype);
      deprecatedAPIs.isPureReactComponent = true;
      var isArrayImpl = Array.isArray, REACT_CLIENT_REFERENCE = Symbol.for("react.client.reference"), ReactSharedInternals = {
        H: null,
        A: null,
        T: null,
        S: null,
        actQueue: null,
        asyncTransitions: 0,
        isBatchingLegacy: false,
        didScheduleLegacyUpdate: false,
        didUsePromise: false,
        thrownErrors: [],
        getCurrentStack: null,
        recentlyCreatedOwnerStacks: 0
      }, hasOwnProperty = Object.prototype.hasOwnProperty, createTask = console.createTask ? console.createTask : function() {
        return null;
      };
      deprecatedAPIs = {
        react_stack_bottom_frame: function(callStackForError) {
          return callStackForError();
        }
      };
      var specialPropKeyWarningShown, didWarnAboutOldJSXRuntime;
      var didWarnAboutElementRef = {};
      var unknownOwnerDebugStack = deprecatedAPIs.react_stack_bottom_frame.bind(
        deprecatedAPIs,
        UnknownOwner
      )();
      var unknownOwnerDebugTask = createTask(getTaskName(UnknownOwner));
      var didWarnAboutMaps = false, userProvidedKeyEscapeRegex = /\/+/g, reportGlobalError = "function" === typeof reportError ? reportError : function(error) {
        if ("object" === typeof window && "function" === typeof window.ErrorEvent) {
          var event = new window.ErrorEvent("error", {
            bubbles: true,
            cancelable: true,
            message: "object" === typeof error && null !== error && "string" === typeof error.message ? String(error.message) : String(error),
            error
          });
          if (!window.dispatchEvent(event)) return;
        } else if ("object" === typeof process && "function" === typeof process.emit) {
          process.emit("uncaughtException", error);
          return;
        }
        console.error(error);
      }, didWarnAboutMessageChannel = false, enqueueTaskImpl = null, actScopeDepth = 0, didWarnNoAwaitAct = false, isFlushing = false, queueSeveralMicrotasks = "function" === typeof queueMicrotask ? function(callback) {
        queueMicrotask(function() {
          return queueMicrotask(callback);
        });
      } : enqueueTask;
      deprecatedAPIs = Object.freeze({
        __proto__: null,
        c: function(size) {
          return resolveDispatcher().useMemoCache(size);
        }
      });
      var fnName = {
        map: mapChildren,
        forEach: function(children, forEachFunc, forEachContext) {
          mapChildren(
            children,
            function() {
              forEachFunc.apply(this, arguments);
            },
            forEachContext
          );
        },
        count: function(children) {
          var n = 0;
          mapChildren(children, function() {
            n++;
          });
          return n;
        },
        toArray: function(children) {
          return mapChildren(children, function(child) {
            return child;
          }) || [];
        },
        only: function(children) {
          if (!isValidElement2(children))
            throw Error(
              "React.Children.only expected to receive a single React element child."
            );
          return children;
        }
      };
      exports.Activity = REACT_ACTIVITY_TYPE;
      exports.Children = fnName;
      exports.Component = Component;
      exports.Fragment = REACT_FRAGMENT_TYPE;
      exports.Profiler = REACT_PROFILER_TYPE;
      exports.PureComponent = PureComponent;
      exports.StrictMode = REACT_STRICT_MODE_TYPE;
      exports.Suspense = REACT_SUSPENSE_TYPE;
      exports.__CLIENT_INTERNALS_DO_NOT_USE_OR_WARN_USERS_THEY_CANNOT_UPGRADE = ReactSharedInternals;
      exports.__COMPILER_RUNTIME = deprecatedAPIs;
      exports.act = function(callback) {
        var prevActQueue = ReactSharedInternals.actQueue, prevActScopeDepth = actScopeDepth;
        actScopeDepth++;
        var queue = ReactSharedInternals.actQueue = null !== prevActQueue ? prevActQueue : [], didAwaitActCall = false;
        try {
          var result = callback();
        } catch (error) {
          ReactSharedInternals.thrownErrors.push(error);
        }
        if (0 < ReactSharedInternals.thrownErrors.length)
          throw popActScope(prevActQueue, prevActScopeDepth), callback = aggregateErrors(ReactSharedInternals.thrownErrors), ReactSharedInternals.thrownErrors.length = 0, callback;
        if (null !== result && "object" === typeof result && "function" === typeof result.then) {
          var thenable = result;
          queueSeveralMicrotasks(function() {
            didAwaitActCall || didWarnNoAwaitAct || (didWarnNoAwaitAct = true, console.error(
              "You called act(async () => ...) without await. This could lead to unexpected testing behaviour, interleaving multiple act calls and mixing their scopes. You should - await act(async () => ...);"
            ));
          });
          return {
            then: function(resolve, reject) {
              didAwaitActCall = true;
              thenable.then(
                function(returnValue) {
                  popActScope(prevActQueue, prevActScopeDepth);
                  if (0 === prevActScopeDepth) {
                    try {
                      flushActQueue(queue), enqueueTask(function() {
                        return recursivelyFlushAsyncActWork(
                          returnValue,
                          resolve,
                          reject
                        );
                      });
                    } catch (error$0) {
                      ReactSharedInternals.thrownErrors.push(error$0);
                    }
                    if (0 < ReactSharedInternals.thrownErrors.length) {
                      var _thrownError = aggregateErrors(
                        ReactSharedInternals.thrownErrors
                      );
                      ReactSharedInternals.thrownErrors.length = 0;
                      reject(_thrownError);
                    }
                  } else resolve(returnValue);
                },
                function(error) {
                  popActScope(prevActQueue, prevActScopeDepth);
                  0 < ReactSharedInternals.thrownErrors.length ? (error = aggregateErrors(
                    ReactSharedInternals.thrownErrors
                  ), ReactSharedInternals.thrownErrors.length = 0, reject(error)) : reject(error);
                }
              );
            }
          };
        }
        var returnValue$jscomp$0 = result;
        popActScope(prevActQueue, prevActScopeDepth);
        0 === prevActScopeDepth && (flushActQueue(queue), 0 !== queue.length && queueSeveralMicrotasks(function() {
          didAwaitActCall || didWarnNoAwaitAct || (didWarnNoAwaitAct = true, console.error(
            "A component suspended inside an `act` scope, but the `act` call was not awaited. When testing React components that depend on asynchronous data, you must await the result:\n\nawait act(() => ...)"
          ));
        }), ReactSharedInternals.actQueue = null);
        if (0 < ReactSharedInternals.thrownErrors.length)
          throw callback = aggregateErrors(ReactSharedInternals.thrownErrors), ReactSharedInternals.thrownErrors.length = 0, callback;
        return {
          then: function(resolve, reject) {
            didAwaitActCall = true;
            0 === prevActScopeDepth ? (ReactSharedInternals.actQueue = queue, enqueueTask(function() {
              return recursivelyFlushAsyncActWork(
                returnValue$jscomp$0,
                resolve,
                reject
              );
            })) : resolve(returnValue$jscomp$0);
          }
        };
      };
      exports.cache = function(fn) {
        return function() {
          return fn.apply(null, arguments);
        };
      };
      exports.cacheSignal = function() {
        return null;
      };
      exports.captureOwnerStack = function() {
        var getCurrentStack = ReactSharedInternals.getCurrentStack;
        return null === getCurrentStack ? null : getCurrentStack();
      };
      exports.cloneElement = function(element, config, children) {
        if (null === element || void 0 === element)
          throw Error(
            "The argument must be a React element, but you passed " + element + "."
          );
        var props = assign({}, element.props), key = element.key, owner = element._owner;
        if (null != config) {
          var JSCompiler_inline_result;
          a: {
            if (hasOwnProperty.call(config, "ref") && (JSCompiler_inline_result = Object.getOwnPropertyDescriptor(
              config,
              "ref"
            ).get) && JSCompiler_inline_result.isReactWarning) {
              JSCompiler_inline_result = false;
              break a;
            }
            JSCompiler_inline_result = void 0 !== config.ref;
          }
          JSCompiler_inline_result && (owner = getOwner());
          hasValidKey(config) && (checkKeyStringCoercion(config.key), key = "" + config.key);
          for (propName in config)
            !hasOwnProperty.call(config, propName) || "key" === propName || "__self" === propName || "__source" === propName || "ref" === propName && void 0 === config.ref || (props[propName] = config[propName]);
        }
        var propName = arguments.length - 2;
        if (1 === propName) props.children = children;
        else if (1 < propName) {
          JSCompiler_inline_result = Array(propName);
          for (var i = 0; i < propName; i++)
            JSCompiler_inline_result[i] = arguments[i + 2];
          props.children = JSCompiler_inline_result;
        }
        props = ReactElement(
          element.type,
          key,
          props,
          owner,
          element._debugStack,
          element._debugTask
        );
        for (key = 2; key < arguments.length; key++)
          validateChildKeys(arguments[key]);
        return props;
      };
      exports.createContext = function(defaultValue) {
        defaultValue = {
          $$typeof: REACT_CONTEXT_TYPE,
          _currentValue: defaultValue,
          _currentValue2: defaultValue,
          _threadCount: 0,
          Provider: null,
          Consumer: null
        };
        defaultValue.Provider = defaultValue;
        defaultValue.Consumer = {
          $$typeof: REACT_CONSUMER_TYPE,
          _context: defaultValue
        };
        defaultValue._currentRenderer = null;
        defaultValue._currentRenderer2 = null;
        return defaultValue;
      };
      exports.createElement = function(type, config, children) {
        for (var i = 2; i < arguments.length; i++)
          validateChildKeys(arguments[i]);
        i = {};
        var key = null;
        if (null != config)
          for (propName in didWarnAboutOldJSXRuntime || !("__self" in config) || "key" in config || (didWarnAboutOldJSXRuntime = true, console.warn(
            "Your app (or one of its dependencies) is using an outdated JSX transform. Update to the modern JSX transform for faster performance: https://react.dev/link/new-jsx-transform"
          )), hasValidKey(config) && (checkKeyStringCoercion(config.key), key = "" + config.key), config)
            hasOwnProperty.call(config, propName) && "key" !== propName && "__self" !== propName && "__source" !== propName && (i[propName] = config[propName]);
        var childrenLength = arguments.length - 2;
        if (1 === childrenLength) i.children = children;
        else if (1 < childrenLength) {
          for (var childArray = Array(childrenLength), _i = 0; _i < childrenLength; _i++)
            childArray[_i] = arguments[_i + 2];
          Object.freeze && Object.freeze(childArray);
          i.children = childArray;
        }
        if (type && type.defaultProps)
          for (propName in childrenLength = type.defaultProps, childrenLength)
            void 0 === i[propName] && (i[propName] = childrenLength[propName]);
        key && defineKeyPropWarningGetter(
          i,
          "function" === typeof type ? type.displayName || type.name || "Unknown" : type
        );
        var propName = 1e4 > ReactSharedInternals.recentlyCreatedOwnerStacks++;
        return ReactElement(
          type,
          key,
          i,
          getOwner(),
          propName ? Error("react-stack-top-frame") : unknownOwnerDebugStack,
          propName ? createTask(getTaskName(type)) : unknownOwnerDebugTask
        );
      };
      exports.createRef = function() {
        var refObject = { current: null };
        Object.seal(refObject);
        return refObject;
      };
      exports.forwardRef = function(render2) {
        null != render2 && render2.$$typeof === REACT_MEMO_TYPE ? console.error(
          "forwardRef requires a render function but received a `memo` component. Instead of forwardRef(memo(...)), use memo(forwardRef(...))."
        ) : "function" !== typeof render2 ? console.error(
          "forwardRef requires a render function but was given %s.",
          null === render2 ? "null" : typeof render2
        ) : 0 !== render2.length && 2 !== render2.length && console.error(
          "forwardRef render functions accept exactly two parameters: props and ref. %s",
          1 === render2.length ? "Did you forget to use the ref parameter?" : "Any additional parameter will be undefined."
        );
        null != render2 && null != render2.defaultProps && console.error(
          "forwardRef render functions do not support defaultProps. Did you accidentally pass a React component?"
        );
        var elementType = { $$typeof: REACT_FORWARD_REF_TYPE, render: render2 }, ownName;
        Object.defineProperty(elementType, "displayName", {
          enumerable: false,
          configurable: true,
          get: function() {
            return ownName;
          },
          set: function(name) {
            ownName = name;
            render2.name || render2.displayName || (Object.defineProperty(render2, "name", { value: name }), render2.displayName = name);
          }
        });
        return elementType;
      };
      exports.isValidElement = isValidElement2;
      exports.lazy = function(ctor) {
        ctor = { _status: -1, _result: ctor };
        var lazyType = {
          $$typeof: REACT_LAZY_TYPE,
          _payload: ctor,
          _init: lazyInitializer
        }, ioInfo = {
          name: "lazy",
          start: -1,
          end: -1,
          value: null,
          owner: null,
          debugStack: Error("react-stack-top-frame"),
          debugTask: console.createTask ? console.createTask("lazy()") : null
        };
        ctor._ioInfo = ioInfo;
        lazyType._debugInfo = [{ awaited: ioInfo }];
        return lazyType;
      };
      exports.memo = function(type, compare) {
        null == type && console.error(
          "memo: The first argument must be a component. Instead received: %s",
          null === type ? "null" : typeof type
        );
        compare = {
          $$typeof: REACT_MEMO_TYPE,
          type,
          compare: void 0 === compare ? null : compare
        };
        var ownName;
        Object.defineProperty(compare, "displayName", {
          enumerable: false,
          configurable: true,
          get: function() {
            return ownName;
          },
          set: function(name) {
            ownName = name;
            type.name || type.displayName || (Object.defineProperty(type, "name", { value: name }), type.displayName = name);
          }
        });
        return compare;
      };
      exports.startTransition = function(scope) {
        var prevTransition = ReactSharedInternals.T, currentTransition = {};
        currentTransition._updatedFibers = /* @__PURE__ */ new Set();
        ReactSharedInternals.T = currentTransition;
        try {
          var returnValue = scope(), onStartTransitionFinish = ReactSharedInternals.S;
          null !== onStartTransitionFinish && onStartTransitionFinish(currentTransition, returnValue);
          "object" === typeof returnValue && null !== returnValue && "function" === typeof returnValue.then && (ReactSharedInternals.asyncTransitions++, returnValue.then(releaseAsyncTransition, releaseAsyncTransition), returnValue.then(noop, reportGlobalError));
        } catch (error) {
          reportGlobalError(error);
        } finally {
          null === prevTransition && currentTransition._updatedFibers && (scope = currentTransition._updatedFibers.size, currentTransition._updatedFibers.clear(), 10 < scope && console.warn(
            "Detected a large number of updates inside startTransition. If this is due to a subscription please re-write it to use React provided hooks. Otherwise concurrent mode guarantees are off the table."
          )), null !== prevTransition && null !== currentTransition.types && (null !== prevTransition.types && prevTransition.types !== currentTransition.types && console.error(
            "We expected inner Transitions to have transferred the outer types set and that you cannot add to the outer Transition while inside the inner.This is a bug in React."
          ), prevTransition.types = currentTransition.types), ReactSharedInternals.T = prevTransition;
        }
      };
      exports.unstable_useCacheRefresh = function() {
        return resolveDispatcher().useCacheRefresh();
      };
      exports.use = function(usable) {
        return resolveDispatcher().use(usable);
      };
      exports.useActionState = function(action, initialState, permalink) {
        return resolveDispatcher().useActionState(
          action,
          initialState,
          permalink
        );
      };
      exports.useCallback = function(callback, deps) {
        return resolveDispatcher().useCallback(callback, deps);
      };
      exports.useContext = function(Context) {
        var dispatcher = resolveDispatcher();
        Context.$$typeof === REACT_CONSUMER_TYPE && console.error(
          "Calling useContext(Context.Consumer) is not supported and will cause bugs. Did you mean to call useContext(Context) instead?"
        );
        return dispatcher.useContext(Context);
      };
      exports.useDebugValue = function(value, formatterFn) {
        return resolveDispatcher().useDebugValue(value, formatterFn);
      };
      exports.useDeferredValue = function(value, initialValue) {
        return resolveDispatcher().useDeferredValue(value, initialValue);
      };
      exports.useEffect = function(create2, deps) {
        null == create2 && console.warn(
          "React Hook useEffect requires an effect callback. Did you forget to pass a callback to the hook?"
        );
        return resolveDispatcher().useEffect(create2, deps);
      };
      exports.useEffectEvent = function(callback) {
        return resolveDispatcher().useEffectEvent(callback);
      };
      exports.useId = function() {
        return resolveDispatcher().useId();
      };
      exports.useImperativeHandle = function(ref, create2, deps) {
        return resolveDispatcher().useImperativeHandle(ref, create2, deps);
      };
      exports.useInsertionEffect = function(create2, deps) {
        null == create2 && console.warn(
          "React Hook useInsertionEffect requires an effect callback. Did you forget to pass a callback to the hook?"
        );
        return resolveDispatcher().useInsertionEffect(create2, deps);
      };
      exports.useLayoutEffect = function(create2, deps) {
        null == create2 && console.warn(
          "React Hook useLayoutEffect requires an effect callback. Did you forget to pass a callback to the hook?"
        );
        return resolveDispatcher().useLayoutEffect(create2, deps);
      };
      exports.useMemo = function(create2, deps) {
        return resolveDispatcher().useMemo(create2, deps);
      };
      exports.useOptimistic = function(passthrough, reducer) {
        return resolveDispatcher().useOptimistic(passthrough, reducer);
      };
      exports.useReducer = function(reducer, initialArg, init) {
        return resolveDispatcher().useReducer(reducer, initialArg, init);
      };
      exports.useRef = function(initialValue) {
        return resolveDispatcher().useRef(initialValue);
      };
      exports.useState = function(initialState) {
        return resolveDispatcher().useState(initialState);
      };
      exports.useSyncExternalStore = function(subscribe, getSnapshot, getServerSnapshot) {
        return resolveDispatcher().useSyncExternalStore(
          subscribe,
          getSnapshot,
          getServerSnapshot
        );
      };
      exports.useTransition = function() {
        return resolveDispatcher().useTransition();
      };
      exports.version = "19.2.7";
      "undefined" !== typeof __REACT_DEVTOOLS_GLOBAL_HOOK__ && "function" === typeof __REACT_DEVTOOLS_GLOBAL_HOOK__.registerInternalModuleStop && __REACT_DEVTOOLS_GLOBAL_HOOK__.registerInternalModuleStop(Error());
    })();
  }
});

// node_modules/.pnpm/react@19.2.7/node_modules/react/index.js
var require_react = __commonJS({
  "node_modules/.pnpm/react@19.2.7/node_modules/react/index.js"(exports, module) {
    "use strict";
    if (process.env.NODE_ENV === "production") {
      module.exports = require_react_production();
    } else {
      module.exports = require_react_development();
    }
  }
});

// ../../../output/tammy-invoice-demo/seed-demo.ts
import assert2 from "node:assert/strict";
import { randomBytes } from "node:crypto";
import { mkdir, readFile, rm, writeFile } from "node:fs/promises";
import path from "node:path";
import { _electron, expect } from "/Users/benebsworth/projects/tammy/.worktrees/issue/sbr-registration-ready/apps/desktop/node_modules/@playwright/test/index.mjs";

// node_modules/.pnpm/@bufbuild+protobuf@2.12.1/node_modules/@bufbuild/protobuf/dist/esm/is-message.js
function isMessage(arg, schema) {
  const isMessage2 = arg !== null && typeof arg == "object" && "$typeName" in arg && typeof arg.$typeName == "string";
  if (!isMessage2) {
    return false;
  }
  if (schema === void 0) {
    return true;
  }
  return schema.typeName === arg.$typeName;
}

// node_modules/.pnpm/@bufbuild+protobuf@2.12.1/node_modules/@bufbuild/protobuf/dist/esm/descriptors.js
var ScalarType;
(function(ScalarType2) {
  ScalarType2[ScalarType2["DOUBLE"] = 1] = "DOUBLE";
  ScalarType2[ScalarType2["FLOAT"] = 2] = "FLOAT";
  ScalarType2[ScalarType2["INT64"] = 3] = "INT64";
  ScalarType2[ScalarType2["UINT64"] = 4] = "UINT64";
  ScalarType2[ScalarType2["INT32"] = 5] = "INT32";
  ScalarType2[ScalarType2["FIXED64"] = 6] = "FIXED64";
  ScalarType2[ScalarType2["FIXED32"] = 7] = "FIXED32";
  ScalarType2[ScalarType2["BOOL"] = 8] = "BOOL";
  ScalarType2[ScalarType2["STRING"] = 9] = "STRING";
  ScalarType2[ScalarType2["BYTES"] = 12] = "BYTES";
  ScalarType2[ScalarType2["UINT32"] = 13] = "UINT32";
  ScalarType2[ScalarType2["SFIXED32"] = 15] = "SFIXED32";
  ScalarType2[ScalarType2["SFIXED64"] = 16] = "SFIXED64";
  ScalarType2[ScalarType2["SINT32"] = 17] = "SINT32";
  ScalarType2[ScalarType2["SINT64"] = 18] = "SINT64";
})(ScalarType || (ScalarType = {}));

// node_modules/.pnpm/@bufbuild+protobuf@2.12.1/node_modules/@bufbuild/protobuf/dist/esm/wire/varint.js
function varint64read() {
  let lowBits = 0;
  let highBits = 0;
  for (let shift = 0; shift < 28; shift += 7) {
    let b = this.buf[this.pos++];
    lowBits |= (b & 127) << shift;
    if ((b & 128) == 0) {
      this.assertBounds();
      return [lowBits, highBits];
    }
  }
  let middleByte = this.buf[this.pos++];
  lowBits |= (middleByte & 15) << 28;
  highBits = (middleByte & 112) >> 4;
  if ((middleByte & 128) == 0) {
    this.assertBounds();
    return [lowBits, highBits];
  }
  for (let shift = 3; shift <= 31; shift += 7) {
    let b = this.buf[this.pos++];
    highBits |= (b & 127) << shift;
    if ((b & 128) == 0) {
      this.assertBounds();
      return [lowBits, highBits];
    }
  }
  throw new Error("invalid varint");
}
function varint64write(lo, hi, bytes) {
  for (let i = 0; i < 28; i = i + 7) {
    const shift = lo >>> i;
    const hasNext = !(shift >>> 7 == 0 && hi == 0);
    const byte = (hasNext ? shift | 128 : shift) & 255;
    bytes.push(byte);
    if (!hasNext) {
      return;
    }
  }
  const splitBits = lo >>> 28 & 15 | (hi & 7) << 4;
  const hasMoreBits = !(hi >> 3 == 0);
  bytes.push((hasMoreBits ? splitBits | 128 : splitBits) & 255);
  if (!hasMoreBits) {
    return;
  }
  for (let i = 3; i < 31; i = i + 7) {
    const shift = hi >>> i;
    const hasNext = !(shift >>> 7 == 0);
    const byte = (hasNext ? shift | 128 : shift) & 255;
    bytes.push(byte);
    if (!hasNext) {
      return;
    }
  }
  bytes.push(hi >>> 31 & 1);
}
var TWO_PWR_32_DBL = 4294967296;
function int64FromString(dec) {
  const minus = dec[0] === "-";
  if (minus) {
    dec = dec.slice(1);
  }
  const base = 1e6;
  let lowBits = 0;
  let highBits = 0;
  function add1e6digit(begin, end) {
    const digit1e6 = Number(dec.slice(begin, end));
    highBits *= base;
    lowBits = lowBits * base + digit1e6;
    if (lowBits >= TWO_PWR_32_DBL) {
      highBits = highBits + (lowBits / TWO_PWR_32_DBL | 0);
      lowBits = lowBits % TWO_PWR_32_DBL;
    }
  }
  add1e6digit(-24, -18);
  add1e6digit(-18, -12);
  add1e6digit(-12, -6);
  add1e6digit(-6);
  return minus ? negate(lowBits, highBits) : newBits(lowBits, highBits);
}
function int64ToString(lo, hi) {
  let bits = newBits(lo, hi);
  const negative = bits.hi & 2147483648;
  if (negative) {
    bits = negate(bits.lo, bits.hi);
  }
  const result = uInt64ToString(bits.lo, bits.hi);
  return negative ? "-" + result : result;
}
function uInt64ToString(lo, hi) {
  ({ lo, hi } = toUnsigned(lo, hi));
  if (hi <= 2097151) {
    return String(TWO_PWR_32_DBL * hi + lo);
  }
  const low = lo & 16777215;
  const mid = (lo >>> 24 | hi << 8) & 16777215;
  const high = hi >> 16 & 65535;
  let digitA = low + mid * 6777216 + high * 6710656;
  let digitB = mid + high * 8147497;
  let digitC = high * 2;
  const base = 1e7;
  if (digitA >= base) {
    digitB += Math.floor(digitA / base);
    digitA %= base;
  }
  if (digitB >= base) {
    digitC += Math.floor(digitB / base);
    digitB %= base;
  }
  return digitC.toString() + decimalFrom1e7WithLeadingZeros(digitB) + decimalFrom1e7WithLeadingZeros(digitA);
}
function toUnsigned(lo, hi) {
  return { lo: lo >>> 0, hi: hi >>> 0 };
}
function newBits(lo, hi) {
  return { lo: lo | 0, hi: hi | 0 };
}
function negate(lowBits, highBits) {
  highBits = ~highBits;
  if (lowBits) {
    lowBits = ~lowBits + 1;
  } else {
    highBits += 1;
  }
  return newBits(lowBits, highBits);
}
var decimalFrom1e7WithLeadingZeros = (digit1e7) => {
  const partial = String(digit1e7);
  return "0000000".slice(partial.length) + partial;
};
function varint32write(value, bytes) {
  if (value >= 0) {
    while (value > 127) {
      bytes.push(value & 127 | 128);
      value = value >>> 7;
    }
    bytes.push(value);
  } else {
    for (let i = 0; i < 9; i++) {
      bytes.push(value & 127 | 128);
      value = value >> 7;
    }
    bytes.push(1);
  }
}
function varint32read() {
  let b = this.buf[this.pos++];
  let result = b & 127;
  if ((b & 128) == 0) {
    this.assertBounds();
    return result;
  }
  b = this.buf[this.pos++];
  result |= (b & 127) << 7;
  if ((b & 128) == 0) {
    this.assertBounds();
    return result;
  }
  b = this.buf[this.pos++];
  result |= (b & 127) << 14;
  if ((b & 128) == 0) {
    this.assertBounds();
    return result;
  }
  b = this.buf[this.pos++];
  result |= (b & 127) << 21;
  if ((b & 128) == 0) {
    this.assertBounds();
    return result;
  }
  b = this.buf[this.pos++];
  result |= (b & 15) << 28;
  for (let readBytes = 5; (b & 128) !== 0 && readBytes < 10; readBytes++)
    b = this.buf[this.pos++];
  if ((b & 128) != 0)
    throw new Error("invalid varint");
  this.assertBounds();
  return result >>> 0;
}

// node_modules/.pnpm/@bufbuild+protobuf@2.12.1/node_modules/@bufbuild/protobuf/dist/esm/proto-int64.js
var protoInt64 = /* @__PURE__ */ makeInt64Support();
function makeInt64Support() {
  const dv = new DataView(new ArrayBuffer(8));
  const ok = typeof BigInt === "function" && typeof dv.getBigInt64 === "function" && typeof dv.getBigUint64 === "function" && typeof dv.setBigInt64 === "function" && typeof dv.setBigUint64 === "function" && (!!globalThis.Deno || !!globalThis.Bun || typeof process != "object" || typeof process.env != "object" || process.env.BUF_BIGINT_DISABLE !== "1");
  if (ok) {
    const MIN = BigInt("-9223372036854775808");
    const MAX = BigInt("9223372036854775807");
    const UMIN = BigInt("0");
    const UMAX = BigInt("18446744073709551615");
    return {
      zero: BigInt(0),
      supported: true,
      parse(value) {
        const bi = typeof value == "bigint" ? value : BigInt(value);
        if (bi > MAX || bi < MIN) {
          throw new Error(`invalid int64: ${value}`);
        }
        return bi;
      },
      uParse(value) {
        const bi = typeof value == "bigint" ? value : BigInt(value);
        if (bi > UMAX || bi < UMIN) {
          throw new Error(`invalid uint64: ${value}`);
        }
        return bi;
      },
      enc(value) {
        dv.setBigInt64(0, this.parse(value), true);
        return {
          lo: dv.getInt32(0, true),
          hi: dv.getInt32(4, true)
        };
      },
      uEnc(value) {
        dv.setBigInt64(0, this.uParse(value), true);
        return {
          lo: dv.getInt32(0, true),
          hi: dv.getInt32(4, true)
        };
      },
      dec(lo, hi) {
        dv.setInt32(0, lo, true);
        dv.setInt32(4, hi, true);
        return dv.getBigInt64(0, true);
      },
      uDec(lo, hi) {
        dv.setInt32(0, lo, true);
        dv.setInt32(4, hi, true);
        return dv.getBigUint64(0, true);
      }
    };
  }
  return {
    zero: "0",
    supported: false,
    parse(value) {
      if (typeof value != "string") {
        value = value.toString();
      }
      assertInt64String(value);
      return value;
    },
    uParse(value) {
      if (typeof value != "string") {
        value = value.toString();
      }
      assertUInt64String(value);
      return value;
    },
    enc(value) {
      if (typeof value != "string") {
        value = value.toString();
      }
      assertInt64String(value);
      return int64FromString(value);
    },
    uEnc(value) {
      if (typeof value != "string") {
        value = value.toString();
      }
      assertUInt64String(value);
      return int64FromString(value);
    },
    dec(lo, hi) {
      return int64ToString(lo, hi);
    },
    uDec(lo, hi) {
      return uInt64ToString(lo, hi);
    }
  };
}
function assertInt64String(value) {
  if (!/^-?[0-9]+$/.test(value)) {
    throw new Error("invalid int64: " + value);
  }
}
function assertUInt64String(value) {
  if (!/^[0-9]+$/.test(value)) {
    throw new Error("invalid uint64: " + value);
  }
}

// node_modules/.pnpm/@bufbuild+protobuf@2.12.1/node_modules/@bufbuild/protobuf/dist/esm/reflect/scalar.js
function scalarZeroValue(type, longAsString) {
  switch (type) {
    case ScalarType.STRING:
      return "";
    case ScalarType.BOOL:
      return false;
    case ScalarType.DOUBLE:
    case ScalarType.FLOAT:
      return 0;
    case ScalarType.INT64:
    case ScalarType.UINT64:
    case ScalarType.SFIXED64:
    case ScalarType.FIXED64:
    case ScalarType.SINT64:
      return longAsString ? "0" : protoInt64.zero;
    case ScalarType.BYTES:
      return new Uint8Array(0);
    default:
      return 0;
  }
}
function isScalarZeroValue(type, value) {
  switch (type) {
    case ScalarType.BOOL:
      return value === false;
    case ScalarType.STRING:
      return value === "";
    case ScalarType.BYTES:
      return value instanceof Uint8Array && !value.byteLength;
    case ScalarType.DOUBLE:
    case ScalarType.FLOAT:
      return Object.is(value, 0);
    default:
      return value == 0;
  }
}

// node_modules/.pnpm/@bufbuild+protobuf@2.12.1/node_modules/@bufbuild/protobuf/dist/esm/reflect/unsafe.js
var IMPLICIT = 2;
var unsafeLocal = Symbol.for("reflect unsafe local");
function unsafeOneofCase(target, oneof) {
  const c = target[oneof.localName].case;
  if (c === void 0) {
    return c;
  }
  return oneof.fields.find((f) => f.localName === c);
}
function unsafeIsSet(target, field) {
  const name = field.localName;
  if (field.oneof) {
    return target[field.oneof.localName].case === name;
  }
  if (field.presence != IMPLICIT) {
    return target[name] !== void 0 && Object.prototype.hasOwnProperty.call(target, name);
  }
  switch (field.fieldKind) {
    case "list":
      return target[name].length > 0;
    case "map":
      return Object.keys(target[name]).length > 0;
    case "scalar":
      return !isScalarZeroValue(field.scalar, target[name]);
    case "enum":
      return target[name] !== field.enum.values[0].number;
  }
  throw new Error("message field with implicit presence");
}
function unsafeIsSetExplicit(target, localName) {
  return Object.prototype.hasOwnProperty.call(target, localName) && target[localName] !== void 0;
}
function unsafeGet(target, field) {
  if (field.oneof) {
    const oneof = target[field.oneof.localName];
    if (oneof.case === field.localName) {
      return oneof.value;
    }
    return void 0;
  }
  return target[field.localName];
}
function unsafeSet(target, field, value) {
  if (field.oneof) {
    target[field.oneof.localName] = {
      case: field.localName,
      value
    };
  } else {
    target[field.localName] = value;
  }
}
function unsafeClear(target, field) {
  const name = field.localName;
  if (field.oneof) {
    const oneofLocalName = field.oneof.localName;
    if (target[oneofLocalName].case === name) {
      target[oneofLocalName] = { case: void 0 };
    }
  } else if (field.presence != IMPLICIT) {
    delete target[name];
  } else {
    switch (field.fieldKind) {
      case "map":
        target[name] = {};
        break;
      case "list":
        target[name] = [];
        break;
      case "enum":
        target[name] = field.enum.values[0].number;
        break;
      case "scalar":
        target[name] = scalarZeroValue(field.scalar, field.longAsString);
        break;
    }
  }
}

// node_modules/.pnpm/@bufbuild+protobuf@2.12.1/node_modules/@bufbuild/protobuf/dist/esm/reflect/guard.js
function isObject(arg) {
  return arg !== null && typeof arg == "object" && !Array.isArray(arg);
}
function isReflectList(arg, field) {
  var _a2, _b, _c, _d;
  if (isObject(arg) && unsafeLocal in arg && "add" in arg && "field" in arg && typeof arg.field == "function") {
    if (field !== void 0) {
      const a = field;
      const b = arg.field();
      return a.listKind == b.listKind && a.scalar === b.scalar && ((_a2 = a.message) === null || _a2 === void 0 ? void 0 : _a2.typeName) === ((_b = b.message) === null || _b === void 0 ? void 0 : _b.typeName) && ((_c = a.enum) === null || _c === void 0 ? void 0 : _c.typeName) === ((_d = b.enum) === null || _d === void 0 ? void 0 : _d.typeName);
    }
    return true;
  }
  return false;
}
function isReflectMap(arg, field) {
  var _a2, _b, _c, _d;
  if (isObject(arg) && unsafeLocal in arg && "has" in arg && "field" in arg && typeof arg.field == "function") {
    if (field !== void 0) {
      const a = field, b = arg.field();
      return a.mapKey === b.mapKey && a.mapKind == b.mapKind && a.scalar === b.scalar && ((_a2 = a.message) === null || _a2 === void 0 ? void 0 : _a2.typeName) === ((_b = b.message) === null || _b === void 0 ? void 0 : _b.typeName) && ((_c = a.enum) === null || _c === void 0 ? void 0 : _c.typeName) === ((_d = b.enum) === null || _d === void 0 ? void 0 : _d.typeName);
    }
    return true;
  }
  return false;
}
function isReflectMessage(arg, messageDesc2) {
  return isObject(arg) && unsafeLocal in arg && "desc" in arg && isObject(arg.desc) && arg.desc.kind === "message" && (messageDesc2 === void 0 || arg.desc.typeName == messageDesc2.typeName);
}

// node_modules/.pnpm/@bufbuild+protobuf@2.12.1/node_modules/@bufbuild/protobuf/dist/esm/wkt/wrappers.js
function isWrapper(arg) {
  return isWrapperTypeName(arg.$typeName);
}
function isWrapperDesc(messageDesc2) {
  const f = messageDesc2.fields[0];
  return isWrapperTypeName(messageDesc2.typeName) && f !== void 0 && f.fieldKind == "scalar" && f.name == "value" && f.number == 1;
}
function isWrapperTypeName(name) {
  return name.startsWith("google.protobuf.") && [
    "DoubleValue",
    "FloatValue",
    "Int64Value",
    "UInt64Value",
    "Int32Value",
    "UInt32Value",
    "BoolValue",
    "StringValue",
    "BytesValue"
  ].includes(name.substring(16));
}

// node_modules/.pnpm/@bufbuild+protobuf@2.12.1/node_modules/@bufbuild/protobuf/dist/esm/create.js
var EDITION_PROTO3 = 999;
var EDITION_PROTO2 = 998;
var IMPLICIT2 = 2;
function create(schema, init) {
  if (isMessage(init, schema)) {
    return init;
  }
  const message = createZeroMessage(schema);
  if (init !== void 0) {
    initMessage(schema, message, init);
  }
  return message;
}
function initMessage(messageDesc2, message, init) {
  for (const member of messageDesc2.members) {
    let value = init[member.localName];
    if (value == null) {
      continue;
    }
    let field;
    if (member.kind == "oneof") {
      const oneofField = unsafeOneofCase(init, member);
      if (!oneofField) {
        continue;
      }
      field = oneofField;
      value = unsafeGet(init, oneofField);
    } else {
      field = member;
    }
    switch (field.fieldKind) {
      case "message":
        value = toMessage(field, value);
        break;
      case "scalar":
        value = initScalar(field, value);
        break;
      case "list":
        value = initList(field, value);
        break;
      case "map":
        value = initMap(field, value);
        break;
    }
    unsafeSet(message, field, value);
  }
  return message;
}
function initScalar(field, value) {
  if (field.scalar == ScalarType.BYTES) {
    return toU8Arr(value);
  }
  return value;
}
function initMap(field, value) {
  if (isObject(value)) {
    if (field.scalar == ScalarType.BYTES) {
      return convertObjectValues(value, toU8Arr);
    }
    if (field.mapKind == "message") {
      return convertObjectValues(value, (val) => toMessage(field, val));
    }
  }
  return value;
}
function initList(field, value) {
  if (Array.isArray(value)) {
    if (field.scalar == ScalarType.BYTES) {
      return value.map(toU8Arr);
    }
    if (field.listKind == "message") {
      return value.map((item) => toMessage(field, item));
    }
  }
  return value;
}
function toMessage(field, value) {
  if (field.fieldKind == "message" && !field.oneof && isWrapperDesc(field.message)) {
    return initScalar(field.message.fields[0], value);
  }
  if (isObject(value)) {
    if (field.message.typeName == "google.protobuf.Struct" && field.parent.typeName !== "google.protobuf.Value") {
      return value;
    }
    if (!isMessage(value, field.message)) {
      return create(field.message, value);
    }
  }
  return value;
}
function toU8Arr(value) {
  return Array.isArray(value) ? new Uint8Array(value) : value;
}
function convertObjectValues(obj, fn) {
  const ret = {};
  for (const entry of Object.entries(obj)) {
    ret[entry[0]] = fn(entry[1]);
  }
  return ret;
}
var tokenZeroMessageField = Symbol();
var messagePrototypes = /* @__PURE__ */ new WeakMap();
function createZeroMessage(desc) {
  let msg;
  if (!needsPrototypeChain(desc)) {
    msg = {
      $typeName: desc.typeName
    };
    for (const member of desc.members) {
      if (member.kind == "oneof" || member.presence == IMPLICIT2) {
        msg[member.localName] = createZeroField(member);
      }
    }
  } else {
    const cached = messagePrototypes.get(desc);
    let prototype;
    let members;
    if (cached) {
      ({ prototype, members } = cached);
    } else {
      prototype = {};
      members = /* @__PURE__ */ new Set();
      for (const member of desc.members) {
        if (member.kind == "oneof") {
          continue;
        }
        if (member.fieldKind != "scalar" && member.fieldKind != "enum") {
          continue;
        }
        if (member.presence == IMPLICIT2) {
          continue;
        }
        members.add(member);
        prototype[member.localName] = createZeroField(member);
      }
      messagePrototypes.set(desc, { prototype, members });
    }
    msg = Object.create(prototype);
    msg.$typeName = desc.typeName;
    for (const member of desc.members) {
      if (members.has(member)) {
        continue;
      }
      if (member.kind == "field") {
        if (member.fieldKind == "message") {
          continue;
        }
        if (member.fieldKind == "scalar" || member.fieldKind == "enum") {
          if (member.presence != IMPLICIT2) {
            continue;
          }
        }
      }
      msg[member.localName] = createZeroField(member);
    }
  }
  return msg;
}
function needsPrototypeChain(desc) {
  switch (desc.file.edition) {
    case EDITION_PROTO3:
      return false;
    case EDITION_PROTO2:
      return true;
    default:
      return desc.fields.some((f) => f.presence != IMPLICIT2 && f.fieldKind != "message" && !f.oneof);
  }
}
function createZeroField(field) {
  if (field.kind == "oneof") {
    return { case: void 0 };
  }
  if (field.fieldKind == "list") {
    return [];
  }
  if (field.fieldKind == "map") {
    return {};
  }
  if (field.fieldKind == "message") {
    return tokenZeroMessageField;
  }
  const defaultValue = field.getDefaultValue();
  if (defaultValue !== void 0) {
    return field.fieldKind == "scalar" && field.longAsString ? defaultValue.toString() : defaultValue;
  }
  return field.fieldKind == "scalar" ? scalarZeroValue(field.scalar, field.longAsString) : field.enum.values[0].number;
}

// node_modules/.pnpm/@bufbuild+protobuf@2.12.1/node_modules/@bufbuild/protobuf/dist/esm/reflect/error.js
var FieldError = class extends Error {
  constructor(fieldOrOneof, message, name = "FieldValueInvalidError") {
    super(message);
    this.name = name;
    this.field = () => fieldOrOneof;
  }
};

// node_modules/.pnpm/@bufbuild+protobuf@2.12.1/node_modules/@bufbuild/protobuf/dist/esm/wire/text-encoding.js
var symbol = Symbol.for("@bufbuild/protobuf/text-encoding");
function getTextEncoding() {
  if (globalThis[symbol] == void 0) {
    const te = new globalThis.TextEncoder();
    const td = new globalThis.TextDecoder();
    let tdStrict;
    globalThis[symbol] = {
      encodeUtf8(text2) {
        return te.encode(text2);
      },
      decodeUtf8(bytes, strict) {
        if (strict) {
          if (tdStrict === void 0) {
            tdStrict = new globalThis.TextDecoder("utf-8", { fatal: true });
          }
          return tdStrict.decode(bytes);
        }
        return td.decode(bytes);
      },
      checkUtf8(text2) {
        try {
          encodeURIComponent(text2);
          return true;
        } catch (_) {
          return false;
        }
      }
    };
  }
  return globalThis[symbol];
}

// node_modules/.pnpm/@bufbuild+protobuf@2.12.1/node_modules/@bufbuild/protobuf/dist/esm/wire/binary-encoding.js
var WireType;
(function(WireType2) {
  WireType2[WireType2["Varint"] = 0] = "Varint";
  WireType2[WireType2["Bit64"] = 1] = "Bit64";
  WireType2[WireType2["LengthDelimited"] = 2] = "LengthDelimited";
  WireType2[WireType2["StartGroup"] = 3] = "StartGroup";
  WireType2[WireType2["EndGroup"] = 4] = "EndGroup";
  WireType2[WireType2["Bit32"] = 5] = "Bit32";
})(WireType || (WireType = {}));
var FLOAT32_MAX = 34028234663852886e22;
var FLOAT32_MIN = -34028234663852886e22;
var UINT32_MAX = 4294967295;
var INT32_MAX = 2147483647;
var INT32_MIN = -2147483648;
var BinaryWriter = class {
  constructor(encodeUtf8 = getTextEncoding().encodeUtf8) {
    this.encodeUtf8 = encodeUtf8;
    this.stack = [];
    this.chunks = [];
    this.buf = [];
  }
  /**
   * Return all bytes written and reset this writer.
   */
  finish() {
    if (this.buf.length) {
      this.chunks.push(new Uint8Array(this.buf));
      this.buf = [];
    }
    let len = 0;
    for (let i = 0; i < this.chunks.length; i++)
      len += this.chunks[i].length;
    let bytes = new Uint8Array(len);
    let offset = 0;
    for (let i = 0; i < this.chunks.length; i++) {
      bytes.set(this.chunks[i], offset);
      offset += this.chunks[i].length;
    }
    this.chunks = [];
    return bytes;
  }
  /**
   * Start a new fork for length-delimited data like a message
   * or a packed repeated field.
   *
   * Must be joined later with `join()`.
   */
  fork() {
    this.stack.push({ chunks: this.chunks, buf: this.buf });
    this.chunks = [];
    this.buf = [];
    return this;
  }
  /**
   * Join the last fork. Write its length and bytes, then
   * return to the previous state.
   */
  join() {
    let chunk = this.finish();
    let prev = this.stack.pop();
    if (!prev)
      throw new Error("invalid state, fork stack empty");
    this.chunks = prev.chunks;
    this.buf = prev.buf;
    this.uint32(chunk.byteLength);
    return this.raw(chunk);
  }
  /**
   * Writes a tag (field number and wire type).
   *
   * Equivalent to `uint32( (fieldNo << 3 | type) >>> 0 )`.
   *
   * Generated code should compute the tag ahead of time and call `uint32()`.
   */
  tag(fieldNo, type) {
    return this.uint32((fieldNo << 3 | type) >>> 0);
  }
  /**
   * Write a chunk of raw bytes.
   */
  raw(chunk) {
    if (this.buf.length) {
      this.chunks.push(new Uint8Array(this.buf));
      this.buf = [];
    }
    this.chunks.push(chunk);
    return this;
  }
  /**
   * Write a `uint32` value, an unsigned 32 bit varint.
   */
  uint32(value) {
    assertUInt32(value);
    while (value > 127) {
      this.buf.push(value & 127 | 128);
      value = value >>> 7;
    }
    this.buf.push(value);
    return this;
  }
  /**
   * Write a `int32` value, a signed 32 bit varint.
   */
  int32(value) {
    assertInt32(value);
    varint32write(value, this.buf);
    return this;
  }
  /**
   * Write a `bool` value, a varint.
   */
  bool(value) {
    this.buf.push(value ? 1 : 0);
    return this;
  }
  /**
   * Write a `bytes` value, length-delimited arbitrary data.
   */
  bytes(value) {
    this.uint32(value.byteLength);
    return this.raw(value);
  }
  /**
   * Write a `string` value, length-delimited data converted to UTF-8 text.
   */
  string(value) {
    let chunk = this.encodeUtf8(value);
    this.uint32(chunk.byteLength);
    return this.raw(chunk);
  }
  /**
   * Write a `float` value, 32-bit floating point number.
   */
  float(value) {
    assertFloat32(value);
    let chunk = new Uint8Array(4);
    new DataView(chunk.buffer).setFloat32(0, value, true);
    return this.raw(chunk);
  }
  /**
   * Write a `double` value, a 64-bit floating point number.
   */
  double(value) {
    let chunk = new Uint8Array(8);
    new DataView(chunk.buffer).setFloat64(0, value, true);
    return this.raw(chunk);
  }
  /**
   * Write a `fixed32` value, an unsigned, fixed-length 32-bit integer.
   */
  fixed32(value) {
    assertUInt32(value);
    let chunk = new Uint8Array(4);
    new DataView(chunk.buffer).setUint32(0, value, true);
    return this.raw(chunk);
  }
  /**
   * Write a `sfixed32` value, a signed, fixed-length 32-bit integer.
   */
  sfixed32(value) {
    assertInt32(value);
    let chunk = new Uint8Array(4);
    new DataView(chunk.buffer).setInt32(0, value, true);
    return this.raw(chunk);
  }
  /**
   * Write a `sint32` value, a signed, zigzag-encoded 32-bit varint.
   */
  sint32(value) {
    assertInt32(value);
    value = (value << 1 ^ value >> 31) >>> 0;
    varint32write(value, this.buf);
    return this;
  }
  /**
   * Write a `sfixed64` value, a signed, fixed-length 64-bit integer.
   */
  sfixed64(value) {
    let chunk = new Uint8Array(8), view = new DataView(chunk.buffer), tc = protoInt64.enc(value);
    view.setInt32(0, tc.lo, true);
    view.setInt32(4, tc.hi, true);
    return this.raw(chunk);
  }
  /**
   * Write a `fixed64` value, an unsigned, fixed-length 64 bit integer.
   */
  fixed64(value) {
    let chunk = new Uint8Array(8), view = new DataView(chunk.buffer), tc = protoInt64.uEnc(value);
    view.setInt32(0, tc.lo, true);
    view.setInt32(4, tc.hi, true);
    return this.raw(chunk);
  }
  /**
   * Write a `int64` value, a signed 64-bit varint.
   */
  int64(value) {
    let tc = protoInt64.enc(value);
    varint64write(tc.lo, tc.hi, this.buf);
    return this;
  }
  /**
   * Write a `sint64` value, a signed, zig-zag-encoded 64-bit varint.
   */
  sint64(value) {
    const tc = protoInt64.enc(value), sign = tc.hi >> 31, lo = tc.lo << 1 ^ sign, hi = (tc.hi << 1 | tc.lo >>> 31) ^ sign;
    varint64write(lo, hi, this.buf);
    return this;
  }
  /**
   * Write a `uint64` value, an unsigned 64-bit varint.
   */
  uint64(value) {
    const tc = protoInt64.uEnc(value);
    varint64write(tc.lo, tc.hi, this.buf);
    return this;
  }
};
var BinaryReader = class {
  constructor(buf, decodeUtf8 = getTextEncoding().decodeUtf8) {
    this.decodeUtf8 = decodeUtf8;
    this.varint64 = varint64read;
    this.uint32 = varint32read;
    this.buf = buf;
    this.len = buf.length;
    this.pos = 0;
    this.view = new DataView(buf.buffer, buf.byteOffset, buf.byteLength);
  }
  /**
   * Reads a tag - field number and wire type. Tags are uint32 varints; values
   * that do not fit in uint32 are rejected.
   */
  tag() {
    const start = this.pos;
    const tag = this.uint32();
    const bytesRead = this.pos - start;
    if (bytesRead > 5 || bytesRead == 5 && this.buf[this.pos - 1] > 15) {
      throw new Error("illegal tag: varint overflows uint32");
    }
    const fieldNo = tag >>> 3;
    const wireType = tag & 7;
    if (fieldNo <= 0 || wireType > 5) {
      throw new Error("illegal tag: field no " + fieldNo + " wire type " + wireType);
    }
    return [fieldNo, wireType];
  }
  /**
   * Skip one element and return the skipped data.
   *
   * When skipping StartGroup, provide the tags field number to check for
   * matching field number in the EndGroup tag. Recursion into nested groups
   * is guarded by the `recursionLimit` argument: When the limit is reached,
   * this method throws.
   */
  skip(wireType, fieldNo, recursionLimit = 100) {
    let start = this.pos;
    switch (wireType) {
      case WireType.Varint:
        while (this.buf[this.pos++] & 128) {
        }
        break;
      // @ts-ignore TS7029: Fallthrough case in switch -- ignore instead of expect-error for compiler settings without noFallthroughCasesInSwitch: true
      case WireType.Bit64:
        this.pos += 4;
      case WireType.Bit32:
        this.pos += 4;
        break;
      case WireType.LengthDelimited:
        let len = this.uint32();
        this.pos += len;
        break;
      case WireType.StartGroup:
        if (recursionLimit <= 0) {
          throw new Error("maximum recursion depth reached");
        }
        for (; ; ) {
          const [fn, wt] = this.tag();
          if (wt === WireType.EndGroup) {
            if (fieldNo !== void 0 && fn !== fieldNo) {
              throw new Error("invalid end group tag");
            }
            break;
          }
          this.skip(wt, fn, recursionLimit - 1);
        }
        break;
      default:
        throw new Error("cant skip wire type " + wireType);
    }
    this.assertBounds();
    return this.buf.subarray(start, this.pos);
  }
  /**
   * Throws error if position in byte array is out of range.
   */
  assertBounds() {
    if (this.pos > this.len)
      throw new RangeError("premature EOF");
  }
  /**
   * Read a `int32` field, a signed 32 bit varint.
   */
  int32() {
    return this.uint32() | 0;
  }
  /**
   * Read a `sint32` field, a signed, zigzag-encoded 32-bit varint.
   */
  sint32() {
    let zze = this.uint32();
    return zze >>> 1 ^ -(zze & 1);
  }
  /**
   * Read a `int64` field, a signed 64-bit varint.
   */
  int64() {
    return protoInt64.dec(...this.varint64());
  }
  /**
   * Read a `uint64` field, an unsigned 64-bit varint.
   */
  uint64() {
    return protoInt64.uDec(...this.varint64());
  }
  /**
   * Read a `sint64` field, a signed, zig-zag-encoded 64-bit varint.
   */
  sint64() {
    let [lo, hi] = this.varint64();
    let s = -(lo & 1);
    lo = (lo >>> 1 | (hi & 1) << 31) ^ s;
    hi = hi >>> 1 ^ s;
    return protoInt64.dec(lo, hi);
  }
  /**
   * Read a `bool` field, a variant.
   */
  bool() {
    let [lo, hi] = this.varint64();
    return lo !== 0 || hi !== 0;
  }
  /**
   * Read a `fixed32` field, an unsigned, fixed-length 32-bit integer.
   */
  fixed32() {
    return this.view.getUint32((this.pos += 4) - 4, true);
  }
  /**
   * Read a `sfixed32` field, a signed, fixed-length 32-bit integer.
   */
  sfixed32() {
    return this.view.getInt32((this.pos += 4) - 4, true);
  }
  /**
   * Read a `fixed64` field, an unsigned, fixed-length 64 bit integer.
   */
  fixed64() {
    return protoInt64.uDec(this.sfixed32(), this.sfixed32());
  }
  /**
   * Read a `fixed64` field, a signed, fixed-length 64-bit integer.
   */
  sfixed64() {
    return protoInt64.dec(this.sfixed32(), this.sfixed32());
  }
  /**
   * Read a `float` field, 32-bit floating point number.
   */
  float() {
    return this.view.getFloat32((this.pos += 4) - 4, true);
  }
  /**
   * Read a `double` field, a 64-bit floating point number.
   */
  double() {
    return this.view.getFloat64((this.pos += 8) - 8, true);
  }
  /**
   * Read a `bytes` field, length-delimited arbitrary data.
   */
  bytes() {
    let len = this.uint32(), start = this.pos;
    this.pos += len;
    this.assertBounds();
    return this.buf.subarray(start, start + len);
  }
  /**
   * Read a `string` field, length-delimited data converted to UTF-8 text. If
   * `strict` is true, throw on invalid UTF-8 instead of substituting U+FFFD.
   */
  string(strict) {
    return this.decodeUtf8(this.bytes(), strict);
  }
};
function assertInt32(arg) {
  if (typeof arg == "string") {
    arg = Number(arg);
  } else if (typeof arg != "number") {
    throw new Error("invalid int32: " + typeof arg);
  }
  if (!Number.isInteger(arg) || arg > INT32_MAX || arg < INT32_MIN)
    throw new Error("invalid int32: " + arg);
}
function assertUInt32(arg) {
  if (typeof arg == "string") {
    arg = Number(arg);
  } else if (typeof arg != "number") {
    throw new Error("invalid uint32: " + typeof arg);
  }
  if (!Number.isInteger(arg) || arg > UINT32_MAX || arg < 0)
    throw new Error("invalid uint32: " + arg);
}
function assertFloat32(arg) {
  if (typeof arg == "string") {
    const o = arg;
    arg = Number(arg);
    if (Number.isNaN(arg) && o !== "NaN") {
      throw new Error("invalid float32: " + o);
    }
  } else if (typeof arg != "number") {
    throw new Error("invalid float32: " + typeof arg);
  }
  if (Number.isFinite(arg) && (arg > FLOAT32_MAX || arg < FLOAT32_MIN))
    throw new Error("invalid float32: " + arg);
}

// node_modules/.pnpm/@bufbuild+protobuf@2.12.1/node_modules/@bufbuild/protobuf/dist/esm/reflect/reflect-check.js
function checkField(field, value) {
  const check = field.fieldKind == "list" ? isReflectList(value, field) : field.fieldKind == "map" ? isReflectMap(value, field) : checkSingular(field, value);
  if (check === true) {
    return void 0;
  }
  let reason;
  switch (field.fieldKind) {
    case "list":
      reason = `expected ${formatReflectList(field)}, got ${formatVal(value)}`;
      break;
    case "map":
      reason = `expected ${formatReflectMap(field)}, got ${formatVal(value)}`;
      break;
    default: {
      reason = reasonSingular(field, value, check);
    }
  }
  return new FieldError(field, reason);
}
function checkListItem(field, index, value) {
  const check = checkSingular(field, value);
  if (check !== true) {
    return new FieldError(field, `list item #${index + 1}: ${reasonSingular(field, value, check)}`);
  }
  return void 0;
}
function checkMapEntry(field, key, value) {
  const checkKey = checkScalarValue(key, field.mapKey);
  if (checkKey !== true) {
    return new FieldError(field, `invalid map key: ${reasonSingular({ scalar: field.mapKey }, key, checkKey)}`);
  }
  const checkVal = checkSingular(field, value);
  if (checkVal !== true) {
    return new FieldError(field, `map entry ${formatVal(key)}: ${reasonSingular(field, value, checkVal)}`);
  }
  return void 0;
}
function checkSingular(field, value) {
  if (field.scalar !== void 0) {
    return checkScalarValue(value, field.scalar);
  }
  if (field.enum !== void 0) {
    if (field.enum.open) {
      return checkScalarValue(value, ScalarType.INT32);
    }
    return field.enum.values.some((v) => v.number === value);
  }
  return isReflectMessage(value, field.message);
}
function checkScalarValue(value, scalar) {
  switch (scalar) {
    case ScalarType.DOUBLE:
      return typeof value == "number";
    case ScalarType.FLOAT:
      if (typeof value != "number") {
        return false;
      }
      if (Number.isNaN(value) || !Number.isFinite(value)) {
        return true;
      }
      if (value > FLOAT32_MAX || value < FLOAT32_MIN) {
        return `${value.toFixed()} out of range`;
      }
      return true;
    case ScalarType.INT32:
    case ScalarType.SFIXED32:
    case ScalarType.SINT32:
      if (typeof value !== "number" || !Number.isInteger(value)) {
        return false;
      }
      if (value > INT32_MAX || value < INT32_MIN) {
        return `${value.toFixed()} out of range`;
      }
      return true;
    case ScalarType.FIXED32:
    case ScalarType.UINT32:
      if (typeof value !== "number" || !Number.isInteger(value)) {
        return false;
      }
      if (value > UINT32_MAX || value < 0) {
        return `${value.toFixed()} out of range`;
      }
      return true;
    case ScalarType.BOOL:
      return typeof value == "boolean";
    case ScalarType.STRING:
      if (typeof value != "string") {
        return false;
      }
      return getTextEncoding().checkUtf8(value) || "invalid UTF8";
    case ScalarType.BYTES:
      return value instanceof Uint8Array;
    case ScalarType.INT64:
    case ScalarType.SFIXED64:
    case ScalarType.SINT64:
      if (typeof value == "bigint" || typeof value == "number" || typeof value == "string" && value.length > 0) {
        try {
          protoInt64.parse(value);
          return true;
        } catch (_) {
          return `${value} out of range`;
        }
      }
      return false;
    case ScalarType.FIXED64:
    case ScalarType.UINT64:
      if (typeof value == "bigint" || typeof value == "number" || typeof value == "string" && value.length > 0) {
        try {
          protoInt64.uParse(value);
          return true;
        } catch (_) {
          return `${value} out of range`;
        }
      }
      return false;
  }
}
function reasonSingular(field, val, details) {
  details = typeof details == "string" ? `: ${details}` : `, got ${formatVal(val)}`;
  if (field.scalar !== void 0) {
    return `expected ${scalarTypeDescription(field.scalar)}` + details;
  }
  if (field.enum !== void 0) {
    return `expected ${field.enum.toString()}` + details;
  }
  return `expected ${formatReflectMessage(field.message)}` + details;
}
function formatVal(val) {
  switch (typeof val) {
    case "object":
      if (val === null) {
        return "null";
      }
      if (val instanceof Uint8Array) {
        return `Uint8Array(${val.length})`;
      }
      if (Array.isArray(val)) {
        return `Array(${val.length})`;
      }
      if (isReflectList(val)) {
        return formatReflectList(val.field());
      }
      if (isReflectMap(val)) {
        return formatReflectMap(val.field());
      }
      if (isReflectMessage(val)) {
        return formatReflectMessage(val.desc);
      }
      if (isMessage(val)) {
        return `message ${val.$typeName}`;
      }
      return "object";
    case "string":
      return val.length > 30 ? "string" : `"${val.split('"').join('\\"')}"`;
    case "boolean":
      return String(val);
    case "number":
      return String(val);
    case "bigint":
      return String(val) + "n";
    default:
      return typeof val;
  }
}
function formatReflectMessage(desc) {
  return `ReflectMessage (${desc.typeName})`;
}
function formatReflectList(field) {
  switch (field.listKind) {
    case "message":
      return `ReflectList (${field.message.toString()})`;
    case "enum":
      return `ReflectList (${field.enum.toString()})`;
    case "scalar":
      return `ReflectList (${ScalarType[field.scalar]})`;
  }
}
function formatReflectMap(field) {
  switch (field.mapKind) {
    case "message":
      return `ReflectMap (${ScalarType[field.mapKey]}, ${field.message.toString()})`;
    case "enum":
      return `ReflectMap (${ScalarType[field.mapKey]}, ${field.enum.toString()})`;
    case "scalar":
      return `ReflectMap (${ScalarType[field.mapKey]}, ${ScalarType[field.scalar]})`;
  }
}
function scalarTypeDescription(scalar) {
  switch (scalar) {
    case ScalarType.STRING:
      return "string";
    case ScalarType.BOOL:
      return "boolean";
    case ScalarType.INT64:
    case ScalarType.SINT64:
    case ScalarType.SFIXED64:
      return "bigint (int64)";
    case ScalarType.UINT64:
    case ScalarType.FIXED64:
      return "bigint (uint64)";
    case ScalarType.BYTES:
      return "Uint8Array";
    case ScalarType.DOUBLE:
      return "number (float64)";
    case ScalarType.FLOAT:
      return "number (float32)";
    case ScalarType.FIXED32:
    case ScalarType.UINT32:
      return "number (uint32)";
    case ScalarType.INT32:
    case ScalarType.SFIXED32:
    case ScalarType.SINT32:
      return "number (int32)";
  }
}

// node_modules/.pnpm/@bufbuild+protobuf@2.12.1/node_modules/@bufbuild/protobuf/dist/esm/reflect/reflect.js
function reflect(messageDesc2, message, check = true) {
  return new ReflectMessageImpl(messageDesc2, message, check);
}
var messageSortedFields = /* @__PURE__ */ new WeakMap();
var ReflectMessageImpl = class {
  get sortedFields() {
    const cached = messageSortedFields.get(this.desc);
    if (cached) {
      return cached;
    }
    const sortedFields = this.desc.fields.concat().sort((a, b) => a.number - b.number);
    messageSortedFields.set(this.desc, sortedFields);
    return sortedFields;
  }
  constructor(messageDesc2, message, check = true) {
    this.lists = /* @__PURE__ */ new Map();
    this.maps = /* @__PURE__ */ new Map();
    this.check = check;
    this.desc = messageDesc2;
    this.message = this[unsafeLocal] = message !== null && message !== void 0 ? message : create(messageDesc2);
    this.fields = messageDesc2.fields;
    this.oneofs = messageDesc2.oneofs;
    this.members = messageDesc2.members;
  }
  findNumber(number) {
    if (!this._fieldsByNumber) {
      this._fieldsByNumber = new Map(this.desc.fields.map((f) => [f.number, f]));
    }
    return this._fieldsByNumber.get(number);
  }
  oneofCase(oneof) {
    assertOwn(this.message, oneof);
    return unsafeOneofCase(this.message, oneof);
  }
  isSet(field) {
    assertOwn(this.message, field);
    return unsafeIsSet(this.message, field);
  }
  clear(field) {
    assertOwn(this.message, field);
    unsafeClear(this.message, field);
  }
  get(field) {
    assertOwn(this.message, field);
    const value = unsafeGet(this.message, field);
    switch (field.fieldKind) {
      case "list":
        let list = this.lists.get(field);
        if (!list || list[unsafeLocal] !== value) {
          this.lists.set(
            field,
            // biome-ignore lint/suspicious/noAssignInExpressions: no
            list = new ReflectListImpl(field, value, this.check)
          );
        }
        return list;
      case "map":
        let map = this.maps.get(field);
        if (!map || map[unsafeLocal] !== value) {
          this.maps.set(
            field,
            // biome-ignore lint/suspicious/noAssignInExpressions: no
            map = new ReflectMapImpl(field, value, this.check)
          );
        }
        return map;
      case "message":
        return messageToReflect(field, value, this.check);
      case "scalar":
        return value === void 0 ? scalarZeroValue(field.scalar, false) : longToReflect(field, value);
      case "enum":
        return value !== null && value !== void 0 ? value : field.enum.values[0].number;
    }
  }
  set(field, value) {
    assertOwn(this.message, field);
    if (this.check) {
      const err = checkField(field, value);
      if (err) {
        throw err;
      }
    }
    let local;
    if (field.fieldKind == "message") {
      local = messageToLocal(field, value);
    } else if (isReflectMap(value) || isReflectList(value)) {
      local = value[unsafeLocal];
    } else {
      local = longToLocal(field, value);
    }
    unsafeSet(this.message, field, local);
  }
  getUnknown() {
    return this.message.$unknown;
  }
  setUnknown(value) {
    this.message.$unknown = value;
  }
};
function assertOwn(owner, member) {
  if (member.parent.typeName !== owner.$typeName) {
    throw new FieldError(member, `cannot use ${member.toString()} with message ${owner.$typeName}`, "ForeignFieldError");
  }
}
var ReflectListImpl = class {
  field() {
    return this._field;
  }
  get size() {
    return this._arr.length;
  }
  constructor(field, unsafeInput, check) {
    this._field = field;
    this._arr = this[unsafeLocal] = unsafeInput;
    this.check = check;
  }
  get(index) {
    const item = this._arr[index];
    return item === void 0 ? void 0 : listItemToReflect(this._field, item, this.check);
  }
  set(index, item) {
    if (index < 0 || index >= this._arr.length) {
      throw new FieldError(this._field, `list item #${index + 1}: out of range`);
    }
    if (this.check) {
      const err = checkListItem(this._field, index, item);
      if (err) {
        throw err;
      }
    }
    this._arr[index] = listItemToLocal(this._field, item);
  }
  add(item) {
    if (this.check) {
      const err = checkListItem(this._field, this._arr.length, item);
      if (err) {
        throw err;
      }
    }
    this._arr.push(listItemToLocal(this._field, item));
    return void 0;
  }
  clear() {
    this._arr.splice(0, this._arr.length);
  }
  [Symbol.iterator]() {
    return this.values();
  }
  keys() {
    return this._arr.keys();
  }
  *values() {
    for (const item of this._arr) {
      yield listItemToReflect(this._field, item, this.check);
    }
  }
  *entries() {
    for (let i = 0; i < this._arr.length; i++) {
      yield [i, listItemToReflect(this._field, this._arr[i], this.check)];
    }
  }
};
var ReflectMapImpl = class {
  constructor(field, unsafeInput, check = true) {
    this.obj = this[unsafeLocal] = unsafeInput !== null && unsafeInput !== void 0 ? unsafeInput : {};
    this.check = check;
    this._field = field;
  }
  field() {
    return this._field;
  }
  set(key, value) {
    if (this.check) {
      const err = checkMapEntry(this._field, key, value);
      if (err) {
        throw err;
      }
    }
    this.obj[mapKeyToLocal(key)] = mapValueToLocal(this._field, value);
    return this;
  }
  delete(key) {
    const k = mapKeyToLocal(key);
    const has = Object.prototype.hasOwnProperty.call(this.obj, k);
    if (has) {
      delete this.obj[k];
    }
    return has;
  }
  clear() {
    for (const key of Object.keys(this.obj)) {
      delete this.obj[key];
    }
  }
  get(key) {
    let val = this.obj[mapKeyToLocal(key)];
    if (val !== void 0) {
      val = mapValueToReflect(this._field, val, this.check);
    }
    return val;
  }
  has(key) {
    return Object.prototype.hasOwnProperty.call(this.obj, mapKeyToLocal(key));
  }
  *keys() {
    for (const objKey of Object.keys(this.obj)) {
      yield mapKeyToReflect(objKey, this._field.mapKey);
    }
  }
  *entries() {
    for (const objEntry of Object.entries(this.obj)) {
      yield [
        mapKeyToReflect(objEntry[0], this._field.mapKey),
        mapValueToReflect(this._field, objEntry[1], this.check)
      ];
    }
  }
  [Symbol.iterator]() {
    return this.entries();
  }
  get size() {
    return Object.keys(this.obj).length;
  }
  *values() {
    for (const val of Object.values(this.obj)) {
      yield mapValueToReflect(this._field, val, this.check);
    }
  }
  forEach(callbackfn, thisArg) {
    for (const mapEntry of this.entries()) {
      callbackfn.call(thisArg, mapEntry[1], mapEntry[0], this);
    }
  }
};
function messageToLocal(field, value) {
  if (!isReflectMessage(value)) {
    return value;
  }
  if (isWrapper(value.message) && !field.oneof && field.fieldKind == "message") {
    return value.message.value;
  }
  if (value.desc.typeName == "google.protobuf.Struct" && field.parent.typeName != "google.protobuf.Value") {
    return wktStructToLocal(value.message);
  }
  return value.message;
}
function messageToReflect(field, value, check) {
  if (value !== void 0) {
    if (isWrapperDesc(field.message) && !field.oneof && field.fieldKind == "message") {
      value = {
        $typeName: field.message.typeName,
        value: longToReflect(field.message.fields[0], value)
      };
    } else if (field.message.typeName == "google.protobuf.Struct" && field.parent.typeName != "google.protobuf.Value" && isObject(value)) {
      value = wktStructToReflect(value);
    }
  }
  return new ReflectMessageImpl(field.message, value, check);
}
function listItemToLocal(field, value) {
  if (field.listKind == "message") {
    return messageToLocal(field, value);
  }
  return longToLocal(field, value);
}
function listItemToReflect(field, value, check) {
  if (field.listKind == "message") {
    return messageToReflect(field, value, check);
  }
  return longToReflect(field, value);
}
function mapValueToLocal(field, value) {
  if (field.mapKind == "message") {
    return messageToLocal(field, value);
  }
  return longToLocal(field, value);
}
function mapValueToReflect(field, value, check) {
  if (field.mapKind == "message") {
    return messageToReflect(field, value, check);
  }
  return value;
}
function mapKeyToLocal(key) {
  return typeof key == "string" || typeof key == "number" ? key : String(key);
}
function mapKeyToReflect(key, type) {
  switch (type) {
    case ScalarType.STRING:
      return key;
    case ScalarType.INT32:
    case ScalarType.FIXED32:
    case ScalarType.UINT32:
    case ScalarType.SFIXED32:
    case ScalarType.SINT32: {
      const n = Number.parseInt(key);
      if (Number.isFinite(n)) {
        return n;
      }
      break;
    }
    case ScalarType.BOOL:
      switch (key) {
        case "true":
          return true;
        case "false":
          return false;
      }
      break;
    case ScalarType.UINT64:
    case ScalarType.FIXED64:
      try {
        return protoInt64.uParse(key);
      } catch (_a2) {
      }
      break;
    default:
      try {
        return protoInt64.parse(key);
      } catch (_b) {
      }
      break;
  }
  return key;
}
function longToReflect(field, value) {
  switch (field.scalar) {
    case ScalarType.INT64:
    case ScalarType.SFIXED64:
    case ScalarType.SINT64:
      if ("longAsString" in field && field.longAsString && typeof value == "string") {
        value = protoInt64.parse(value);
      }
      break;
    case ScalarType.FIXED64:
    case ScalarType.UINT64:
      if ("longAsString" in field && field.longAsString && typeof value == "string") {
        value = protoInt64.uParse(value);
      }
      break;
  }
  return value;
}
function longToLocal(field, value) {
  switch (field.scalar) {
    case ScalarType.INT64:
    case ScalarType.SFIXED64:
    case ScalarType.SINT64:
      if ("longAsString" in field && field.longAsString) {
        value = String(value);
      } else if (typeof value == "string" || typeof value == "number") {
        value = protoInt64.parse(value);
      }
      break;
    case ScalarType.FIXED64:
    case ScalarType.UINT64:
      if ("longAsString" in field && field.longAsString) {
        value = String(value);
      } else if (typeof value == "string" || typeof value == "number") {
        value = protoInt64.uParse(value);
      }
      break;
  }
  return value;
}
function wktStructToReflect(json) {
  const struct = {
    $typeName: "google.protobuf.Struct",
    fields: {}
  };
  if (isObject(json)) {
    for (const [k, v] of Object.entries(json)) {
      struct.fields[k] = wktValueToReflect(v);
    }
  }
  return struct;
}
function wktStructToLocal(val) {
  const json = {};
  for (const [k, v] of Object.entries(val.fields)) {
    json[k] = wktValueToLocal(v);
  }
  return json;
}
function wktValueToLocal(val) {
  switch (val.kind.case) {
    case "structValue":
      return wktStructToLocal(val.kind.value);
    case "listValue":
      return val.kind.value.values.map(wktValueToLocal);
    case "nullValue":
    case void 0:
      return null;
    default:
      return val.kind.value;
  }
}
function wktValueToReflect(json) {
  const value = {
    $typeName: "google.protobuf.Value",
    kind: { case: void 0 }
  };
  switch (typeof json) {
    case "number":
      value.kind = { case: "numberValue", value: json };
      break;
    case "string":
      value.kind = { case: "stringValue", value: json };
      break;
    case "boolean":
      value.kind = { case: "boolValue", value: json };
      break;
    case "object":
      if (json === null) {
        const nullValue = 0;
        value.kind = { case: "nullValue", value: nullValue };
      } else if (Array.isArray(json)) {
        const listValue = {
          $typeName: "google.protobuf.ListValue",
          values: []
        };
        if (Array.isArray(json)) {
          for (const e of json) {
            listValue.values.push(wktValueToReflect(e));
          }
        }
        value.kind = {
          case: "listValue",
          value: listValue
        };
      } else {
        value.kind = {
          case: "structValue",
          value: wktStructToReflect(json)
        };
      }
      break;
  }
  return value;
}

// node_modules/.pnpm/@bufbuild+protobuf@2.12.1/node_modules/@bufbuild/protobuf/dist/esm/wire/base64-encoding.js
function base64Decode(base64Str) {
  const table = getDecodeTable();
  let es = base64Str.length * 3 / 4;
  if (base64Str[base64Str.length - 2] == "=")
    es -= 2;
  else if (base64Str[base64Str.length - 1] == "=")
    es -= 1;
  let bytes = new Uint8Array(es), bytePos = 0, groupPos = 0, b, p = 0;
  for (let i = 0; i < base64Str.length; i++) {
    b = table[base64Str.charCodeAt(i)];
    if (b === void 0) {
      switch (base64Str[i]) {
        // @ts-ignore TS7029: Fallthrough case in switch -- ignore instead of expect-error for compiler settings without noFallthroughCasesInSwitch: true
        case "=":
          groupPos = 0;
        // reset state when padding found
        case "\n":
        case "\r":
        case "	":
        case " ":
          continue;
        // skip white-space, and padding
        default:
          throw Error("invalid base64 string");
      }
    }
    switch (groupPos) {
      case 0:
        p = b;
        groupPos = 1;
        break;
      case 1:
        bytes[bytePos++] = p << 2 | (b & 48) >> 4;
        p = b;
        groupPos = 2;
        break;
      case 2:
        bytes[bytePos++] = (p & 15) << 4 | (b & 60) >> 2;
        p = b;
        groupPos = 3;
        break;
      case 3:
        bytes[bytePos++] = (p & 3) << 6 | b;
        groupPos = 0;
        break;
    }
  }
  if (groupPos == 1)
    throw Error("invalid base64 string");
  return bytes.subarray(0, bytePos);
}
var encodeTableStd;
var encodeTableUrl;
var decodeTable;
function getEncodeTable(encoding) {
  if (!encodeTableStd) {
    encodeTableStd = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/".split("");
    encodeTableUrl = encodeTableStd.slice(0, -2).concat("-", "_");
  }
  return encoding == "url" ? (
    // biome-ignore lint/style/noNonNullAssertion: TS fails to narrow down
    encodeTableUrl
  ) : encodeTableStd;
}
function getDecodeTable() {
  if (!decodeTable) {
    decodeTable = [];
    const encodeTable = getEncodeTable("std");
    for (let i = 0; i < encodeTable.length; i++)
      decodeTable[encodeTable[i].charCodeAt(0)] = i;
    decodeTable["-".charCodeAt(0)] = encodeTable.indexOf("+");
    decodeTable["_".charCodeAt(0)] = encodeTable.indexOf("/");
  }
  return decodeTable;
}

// node_modules/.pnpm/@bufbuild+protobuf@2.12.1/node_modules/@bufbuild/protobuf/dist/esm/reflect/names.js
function protoCamelCase(snakeCase) {
  let capNext = false;
  const b = [];
  for (let i = 0; i < snakeCase.length; i++) {
    let c = snakeCase.charAt(i);
    switch (c) {
      case "_":
        capNext = true;
        break;
      case "0":
      case "1":
      case "2":
      case "3":
      case "4":
      case "5":
      case "6":
      case "7":
      case "8":
      case "9":
        b.push(c);
        capNext = false;
        break;
      default:
        if (capNext) {
          capNext = false;
          c = c.toUpperCase();
        }
        b.push(c);
        break;
    }
  }
  return b.join("");
}
var reservedObjectProperties = /* @__PURE__ */ new Set([
  // names reserved by JavaScript
  "constructor",
  "toString",
  "toJSON",
  "valueOf"
]);
function safeObjectProperty(name) {
  return reservedObjectProperties.has(name) ? name + "$" : name;
}

// node_modules/.pnpm/@bufbuild+protobuf@2.12.1/node_modules/@bufbuild/protobuf/dist/esm/codegenv2/restore-json-names.js
function restoreJsonNames(message) {
  for (const f of message.field) {
    if (!unsafeIsSetExplicit(f, "jsonName")) {
      f.jsonName = protoCamelCase(f.name);
    }
  }
  message.nestedType.forEach(restoreJsonNames);
}

// node_modules/.pnpm/@bufbuild+protobuf@2.12.1/node_modules/@bufbuild/protobuf/dist/esm/wire/text-format.js
function parseTextFormatEnumValue(descEnum, value) {
  const enumValue = descEnum.values.find((v) => v.name === value);
  if (!enumValue) {
    throw new Error(`cannot parse ${descEnum} default value: ${value}`);
  }
  return enumValue.number;
}
function parseTextFormatScalarValue(type, value) {
  switch (type) {
    case ScalarType.STRING:
      return value;
    case ScalarType.BYTES: {
      const u = unescapeBytesDefaultValue(value);
      if (u === false) {
        throw new Error(`cannot parse ${ScalarType[type]} default value: ${value}`);
      }
      return u;
    }
    case ScalarType.INT64:
    case ScalarType.SFIXED64:
    case ScalarType.SINT64:
      return protoInt64.parse(value);
    case ScalarType.UINT64:
    case ScalarType.FIXED64:
      return protoInt64.uParse(value);
    case ScalarType.DOUBLE:
    case ScalarType.FLOAT:
      switch (value) {
        case "inf":
          return Number.POSITIVE_INFINITY;
        case "-inf":
          return Number.NEGATIVE_INFINITY;
        case "nan":
          return Number.NaN;
        default:
          return parseFloat(value);
      }
    case ScalarType.BOOL:
      return value === "true";
    case ScalarType.INT32:
    case ScalarType.UINT32:
    case ScalarType.SINT32:
    case ScalarType.FIXED32:
    case ScalarType.SFIXED32:
      return parseInt(value, 10);
  }
}
function unescapeBytesDefaultValue(str) {
  const b = [];
  const input = {
    tail: str,
    c: "",
    next() {
      if (this.tail.length == 0) {
        return false;
      }
      this.c = this.tail[0];
      this.tail = this.tail.substring(1);
      return true;
    },
    take(n) {
      if (this.tail.length >= n) {
        const r = this.tail.substring(0, n);
        this.tail = this.tail.substring(n);
        return r;
      }
      return false;
    }
  };
  while (input.next()) {
    switch (input.c) {
      case "\\":
        if (input.next()) {
          switch (input.c) {
            case "\\":
              b.push(input.c.charCodeAt(0));
              break;
            case "b":
              b.push(8);
              break;
            case "f":
              b.push(12);
              break;
            case "n":
              b.push(10);
              break;
            case "r":
              b.push(13);
              break;
            case "t":
              b.push(9);
              break;
            case "v":
              b.push(11);
              break;
            case "0":
            case "1":
            case "2":
            case "3":
            case "4":
            case "5":
            case "6":
            case "7": {
              const s = input.c;
              const t = input.take(2);
              if (t === false) {
                return false;
              }
              const n = parseInt(s + t, 8);
              if (Number.isNaN(n)) {
                return false;
              }
              b.push(n);
              break;
            }
            case "x": {
              const s = input.c;
              const t = input.take(2);
              if (t === false) {
                return false;
              }
              const n = parseInt(s + t, 16);
              if (Number.isNaN(n)) {
                return false;
              }
              b.push(n);
              break;
            }
            case "u": {
              const s = input.c;
              const t = input.take(4);
              if (t === false) {
                return false;
              }
              const n = parseInt(s + t, 16);
              if (Number.isNaN(n)) {
                return false;
              }
              const chunk = new Uint8Array(4);
              const view = new DataView(chunk.buffer);
              view.setInt32(0, n, true);
              b.push(chunk[0], chunk[1], chunk[2], chunk[3]);
              break;
            }
            case "U": {
              const s = input.c;
              const t = input.take(8);
              if (t === false) {
                return false;
              }
              const tc = protoInt64.uEnc(s + t);
              const chunk = new Uint8Array(8);
              const view = new DataView(chunk.buffer);
              view.setInt32(0, tc.lo, true);
              view.setInt32(4, tc.hi, true);
              b.push(chunk[0], chunk[1], chunk[2], chunk[3], chunk[4], chunk[5], chunk[6], chunk[7]);
              break;
            }
          }
        }
        break;
      default:
        b.push(input.c.charCodeAt(0));
    }
  }
  return new Uint8Array(b);
}

// node_modules/.pnpm/@bufbuild+protobuf@2.12.1/node_modules/@bufbuild/protobuf/dist/esm/reflect/nested-types.js
function* nestedTypes(desc) {
  switch (desc.kind) {
    case "file":
      for (const message of desc.messages) {
        yield message;
        yield* nestedTypes(message);
      }
      yield* desc.enums;
      yield* desc.services;
      yield* desc.extensions;
      break;
    case "message":
      for (const message of desc.nestedMessages) {
        yield message;
        yield* nestedTypes(message);
      }
      yield* desc.nestedEnums;
      yield* desc.nestedExtensions;
      break;
  }
}

// node_modules/.pnpm/@bufbuild+protobuf@2.12.1/node_modules/@bufbuild/protobuf/dist/esm/registry.js
function createFileRegistry(...args) {
  const registry = createBaseRegistry();
  if (!args.length) {
    return registry;
  }
  if ("$typeName" in args[0] && args[0].$typeName == "google.protobuf.FileDescriptorSet") {
    for (const file of args[0].file) {
      addFile(file, registry);
    }
    return registry;
  }
  if ("$typeName" in args[0]) {
    let recurseDeps = function(file) {
      const deps = [];
      for (const protoFileName of file.dependency) {
        if (registry.getFile(protoFileName) != void 0) {
          continue;
        }
        if (seen.has(protoFileName)) {
          continue;
        }
        const dep = resolve(protoFileName);
        if (!dep) {
          throw new Error(`Unable to resolve ${protoFileName}, imported by ${file.name}`);
        }
        if ("kind" in dep) {
          registry.addFile(dep, false, true);
        } else {
          seen.add(dep.name);
          deps.push(dep);
        }
      }
      return deps.concat(...deps.map(recurseDeps));
    };
    const input = args[0];
    const resolve = args[1];
    const seen = /* @__PURE__ */ new Set();
    for (const file of [input, ...recurseDeps(input)].reverse()) {
      addFile(file, registry);
    }
  } else {
    for (const fileReg of args) {
      for (const file of fileReg.files) {
        registry.addFile(file);
      }
    }
  }
  return registry;
}
function createBaseRegistry() {
  const types = /* @__PURE__ */ new Map();
  const extendees = /* @__PURE__ */ new Map();
  const files = /* @__PURE__ */ new Map();
  return {
    kind: "registry",
    types,
    extendees,
    [Symbol.iterator]() {
      return types.values();
    },
    get files() {
      return files.values();
    },
    addFile(file, skipTypes, withDeps) {
      files.set(file.proto.name, file);
      if (!skipTypes) {
        for (const type of nestedTypes(file)) {
          this.add(type);
        }
      }
      if (withDeps) {
        for (const f of file.dependencies) {
          this.addFile(f, skipTypes, withDeps);
        }
      }
    },
    add(desc) {
      if (desc.kind == "extension") {
        let numberToExt = extendees.get(desc.extendee.typeName);
        if (!numberToExt) {
          extendees.set(
            desc.extendee.typeName,
            // biome-ignore lint/suspicious/noAssignInExpressions: no
            numberToExt = /* @__PURE__ */ new Map()
          );
        }
        numberToExt.set(desc.number, desc);
      }
      types.set(desc.typeName, desc);
    },
    get(typeName) {
      return types.get(typeName);
    },
    getFile(fileName) {
      return files.get(fileName);
    },
    getMessage(typeName) {
      const t = types.get(typeName);
      return (t === null || t === void 0 ? void 0 : t.kind) == "message" ? t : void 0;
    },
    getEnum(typeName) {
      const t = types.get(typeName);
      return (t === null || t === void 0 ? void 0 : t.kind) == "enum" ? t : void 0;
    },
    getExtension(typeName) {
      const t = types.get(typeName);
      return (t === null || t === void 0 ? void 0 : t.kind) == "extension" ? t : void 0;
    },
    getExtensionFor(extendee, no) {
      var _a2;
      return (_a2 = extendees.get(extendee.typeName)) === null || _a2 === void 0 ? void 0 : _a2.get(no);
    },
    getService(typeName) {
      const t = types.get(typeName);
      return (t === null || t === void 0 ? void 0 : t.kind) == "service" ? t : void 0;
    }
  };
}
var EDITION_PROTO22 = 998;
var EDITION_PROTO32 = 999;
var EDITION_UNSTABLE = 9999;
var TYPE_STRING = 9;
var TYPE_GROUP = 10;
var TYPE_MESSAGE = 11;
var TYPE_BYTES = 12;
var TYPE_ENUM = 14;
var LABEL_REPEATED = 3;
var LABEL_REQUIRED = 2;
var JS_STRING = 1;
var IDEMPOTENCY_UNKNOWN = 0;
var EXPLICIT = 1;
var IMPLICIT3 = 2;
var LEGACY_REQUIRED = 3;
var PACKED = 1;
var DELIMITED = 2;
var OPEN = 1;
var VERIFY = 2;
var maximumEdition = 1001;
var featureDefaults = {
  // EDITION_PROTO2
  998: {
    fieldPresence: 1,
    // EXPLICIT,
    enumType: 2,
    // CLOSED,
    repeatedFieldEncoding: 2,
    // EXPANDED,
    utf8Validation: 3,
    // NONE,
    messageEncoding: 1,
    // LENGTH_PREFIXED,
    jsonFormat: 2,
    // LEGACY_BEST_EFFORT,
    enforceNamingStyle: 2,
    // STYLE_LEGACY,
    defaultSymbolVisibility: 1
    // EXPORT_ALL,
  },
  // EDITION_PROTO3
  999: {
    fieldPresence: 2,
    // IMPLICIT,
    enumType: 1,
    // OPEN,
    repeatedFieldEncoding: 1,
    // PACKED,
    utf8Validation: 2,
    // VERIFY,
    messageEncoding: 1,
    // LENGTH_PREFIXED,
    jsonFormat: 1,
    // ALLOW,
    enforceNamingStyle: 2,
    // STYLE_LEGACY,
    defaultSymbolVisibility: 1
    // EXPORT_ALL,
  },
  // EDITION_2023
  1e3: {
    fieldPresence: 1,
    // EXPLICIT,
    enumType: 1,
    // OPEN,
    repeatedFieldEncoding: 1,
    // PACKED,
    utf8Validation: 2,
    // VERIFY,
    messageEncoding: 1,
    // LENGTH_PREFIXED,
    jsonFormat: 1,
    // ALLOW,
    enforceNamingStyle: 2,
    // STYLE_LEGACY,
    defaultSymbolVisibility: 1
    // EXPORT_ALL,
  },
  // EDITION_2024
  1001: {
    fieldPresence: 1,
    // EXPLICIT,
    enumType: 1,
    // OPEN,
    repeatedFieldEncoding: 1,
    // PACKED,
    utf8Validation: 2,
    // VERIFY,
    messageEncoding: 1,
    // LENGTH_PREFIXED,
    jsonFormat: 1,
    // ALLOW,
    enforceNamingStyle: 1,
    // STYLE2024,
    defaultSymbolVisibility: 2
    // EXPORT_TOP_LEVEL,
  }
};
function addFile(proto, reg) {
  var _a2, _b;
  const file = {
    kind: "file",
    proto,
    deprecated: (_b = (_a2 = proto.options) === null || _a2 === void 0 ? void 0 : _a2.deprecated) !== null && _b !== void 0 ? _b : false,
    edition: getFileEdition(proto),
    name: proto.name.replace(/\.proto$/, ""),
    dependencies: findFileDependencies(proto, reg),
    enums: [],
    messages: [],
    extensions: [],
    services: [],
    toString() {
      return `file ${proto.name}`;
    }
  };
  const mapEntriesStore = /* @__PURE__ */ new Map();
  const mapEntries = {
    get(typeName) {
      return mapEntriesStore.get(typeName);
    },
    add(desc) {
      var _a3;
      assert(((_a3 = desc.proto.options) === null || _a3 === void 0 ? void 0 : _a3.mapEntry) === true);
      mapEntriesStore.set(desc.typeName, desc);
    }
  };
  for (const enumProto of proto.enumType) {
    addEnum(enumProto, file, void 0, reg);
  }
  for (const messageProto of proto.messageType) {
    addMessage(messageProto, file, void 0, reg, mapEntries);
  }
  for (const serviceProto of proto.service) {
    addService(serviceProto, file, reg);
  }
  addExtensions(file, reg);
  for (const mapEntry of mapEntriesStore.values()) {
    addFields(mapEntry, reg, mapEntries);
  }
  for (const message of file.messages) {
    addFields(message, reg, mapEntries);
    addExtensions(message, reg);
  }
  reg.addFile(file, true);
}
function addExtensions(desc, reg) {
  switch (desc.kind) {
    case "file":
      for (const proto of desc.proto.extension) {
        const ext = newField(proto, desc, reg);
        desc.extensions.push(ext);
        reg.add(ext);
      }
      break;
    case "message":
      for (const proto of desc.proto.extension) {
        const ext = newField(proto, desc, reg);
        desc.nestedExtensions.push(ext);
        reg.add(ext);
      }
      for (const message of desc.nestedMessages) {
        addExtensions(message, reg);
      }
      break;
  }
}
function addFields(message, reg, mapEntries) {
  const allOneofs = message.proto.oneofDecl.map((proto) => newOneof(proto, message));
  const oneofsSeen = /* @__PURE__ */ new Set();
  for (const proto of message.proto.field) {
    const oneof = findOneof(proto, allOneofs);
    const field = newField(proto, message, reg, oneof, mapEntries);
    message.fields.push(field);
    message.field[field.localName] = field;
    if (oneof === void 0) {
      message.members.push(field);
    } else {
      oneof.fields.push(field);
      if (!oneofsSeen.has(oneof)) {
        oneofsSeen.add(oneof);
        message.members.push(oneof);
      }
    }
  }
  for (const oneof of allOneofs.filter((o) => oneofsSeen.has(o))) {
    message.oneofs.push(oneof);
  }
  for (const child of message.nestedMessages) {
    addFields(child, reg, mapEntries);
  }
}
function addEnum(proto, file, parent, reg) {
  var _a2, _b, _c, _d, _e;
  const sharedPrefix = findEnumSharedPrefix(proto.name, proto.value);
  const desc = {
    kind: "enum",
    proto,
    deprecated: (_b = (_a2 = proto.options) === null || _a2 === void 0 ? void 0 : _a2.deprecated) !== null && _b !== void 0 ? _b : false,
    file,
    parent,
    open: true,
    name: proto.name,
    typeName: makeTypeName(proto, parent, file),
    value: {},
    values: [],
    sharedPrefix,
    toString() {
      return `enum ${this.typeName}`;
    }
  };
  desc.open = isEnumOpen(desc);
  reg.add(desc);
  for (const p of proto.value) {
    const name = p.name;
    desc.values.push(
      // biome-ignore lint/suspicious/noAssignInExpressions: no
      desc.value[p.number] = {
        kind: "enum_value",
        proto: p,
        deprecated: (_d = (_c = p.options) === null || _c === void 0 ? void 0 : _c.deprecated) !== null && _d !== void 0 ? _d : false,
        parent: desc,
        name,
        localName: safeObjectProperty(sharedPrefix == void 0 ? name : name.substring(sharedPrefix.length)),
        number: p.number,
        toString() {
          return `enum value ${desc.typeName}.${name}`;
        }
      }
    );
  }
  ((_e = parent === null || parent === void 0 ? void 0 : parent.nestedEnums) !== null && _e !== void 0 ? _e : file.enums).push(desc);
}
function addMessage(proto, file, parent, reg, mapEntries) {
  var _a2, _b, _c, _d;
  const desc = {
    kind: "message",
    proto,
    deprecated: (_b = (_a2 = proto.options) === null || _a2 === void 0 ? void 0 : _a2.deprecated) !== null && _b !== void 0 ? _b : false,
    file,
    parent,
    name: proto.name,
    typeName: makeTypeName(proto, parent, file),
    fields: [],
    field: {},
    oneofs: [],
    members: [],
    nestedEnums: [],
    nestedMessages: [],
    nestedExtensions: [],
    toString() {
      return `message ${this.typeName}`;
    }
  };
  if (((_c = proto.options) === null || _c === void 0 ? void 0 : _c.mapEntry) === true) {
    mapEntries.add(desc);
  } else {
    ((_d = parent === null || parent === void 0 ? void 0 : parent.nestedMessages) !== null && _d !== void 0 ? _d : file.messages).push(desc);
    reg.add(desc);
  }
  for (const enumProto of proto.enumType) {
    addEnum(enumProto, file, desc, reg);
  }
  for (const messageProto of proto.nestedType) {
    addMessage(messageProto, file, desc, reg, mapEntries);
  }
}
function addService(proto, file, reg) {
  var _a2, _b;
  const desc = {
    kind: "service",
    proto,
    deprecated: (_b = (_a2 = proto.options) === null || _a2 === void 0 ? void 0 : _a2.deprecated) !== null && _b !== void 0 ? _b : false,
    file,
    name: proto.name,
    typeName: makeTypeName(proto, void 0, file),
    methods: [],
    method: {},
    toString() {
      return `service ${this.typeName}`;
    }
  };
  file.services.push(desc);
  reg.add(desc);
  for (const methodProto of proto.method) {
    const method = newMethod(methodProto, desc, reg);
    desc.methods.push(method);
    desc.method[method.localName] = method;
  }
}
function newMethod(proto, parent, reg) {
  var _a2, _b, _c, _d;
  let methodKind;
  if (proto.clientStreaming && proto.serverStreaming) {
    methodKind = "bidi_streaming";
  } else if (proto.clientStreaming) {
    methodKind = "client_streaming";
  } else if (proto.serverStreaming) {
    methodKind = "server_streaming";
  } else {
    methodKind = "unary";
  }
  const input = reg.getMessage(trimLeadingDot(proto.inputType));
  const output = reg.getMessage(trimLeadingDot(proto.outputType));
  assert(input, `invalid MethodDescriptorProto: input_type ${proto.inputType} not found`);
  assert(output, `invalid MethodDescriptorProto: output_type ${proto.inputType} not found`);
  const name = proto.name;
  return {
    kind: "rpc",
    proto,
    deprecated: (_b = (_a2 = proto.options) === null || _a2 === void 0 ? void 0 : _a2.deprecated) !== null && _b !== void 0 ? _b : false,
    parent,
    name,
    localName: safeObjectProperty(name.length ? safeObjectProperty(name[0].toLowerCase() + name.substring(1)) : name),
    methodKind,
    input,
    output,
    idempotency: (_d = (_c = proto.options) === null || _c === void 0 ? void 0 : _c.idempotencyLevel) !== null && _d !== void 0 ? _d : IDEMPOTENCY_UNKNOWN,
    toString() {
      return `rpc ${parent.typeName}.${name}`;
    }
  };
}
function newOneof(proto, parent) {
  return {
    kind: "oneof",
    proto,
    deprecated: false,
    parent,
    fields: [],
    name: proto.name,
    localName: safeObjectProperty(protoCamelCase(proto.name)),
    toString() {
      return `oneof ${parent.typeName}.${this.name}`;
    }
  };
}
function newField(proto, parentOrFile, reg, oneof, mapEntries) {
  var _a2, _b, _c;
  const isExtension = mapEntries === void 0;
  const field = {
    kind: "field",
    proto,
    deprecated: (_b = (_a2 = proto.options) === null || _a2 === void 0 ? void 0 : _a2.deprecated) !== null && _b !== void 0 ? _b : false,
    name: proto.name,
    number: proto.number,
    scalar: void 0,
    message: void 0,
    enum: void 0,
    presence: getFieldPresence(proto, oneof, isExtension, parentOrFile),
    utf8Validation: isUtf8Validated(proto, parentOrFile),
    listKind: void 0,
    mapKind: void 0,
    mapKey: void 0,
    delimitedEncoding: void 0,
    packed: void 0,
    longAsString: false,
    getDefaultValue: void 0
  };
  if (isExtension) {
    const file = parentOrFile.kind == "file" ? parentOrFile : parentOrFile.file;
    const parent = parentOrFile.kind == "file" ? void 0 : parentOrFile;
    const typeName = makeTypeName(proto, parent, file);
    field.kind = "extension";
    field.file = file;
    field.parent = parent;
    field.oneof = void 0;
    field.typeName = typeName;
    field.jsonName = `[${typeName}]`;
    field.toString = () => `extension ${typeName}`;
    const extendee = reg.getMessage(trimLeadingDot(proto.extendee));
    assert(extendee, `invalid FieldDescriptorProto: extendee ${proto.extendee} not found`);
    field.extendee = extendee;
  } else {
    const parent = parentOrFile;
    assert(parent.kind == "message");
    field.parent = parent;
    field.oneof = oneof;
    field.localName = oneof ? protoCamelCase(proto.name) : safeObjectProperty(protoCamelCase(proto.name));
    field.jsonName = proto.jsonName;
    field.toString = () => `field ${parent.typeName}.${proto.name}`;
  }
  const label = proto.label;
  const type = proto.type;
  const jstype = (_c = proto.options) === null || _c === void 0 ? void 0 : _c.jstype;
  if (label === LABEL_REPEATED) {
    const mapEntry = type == TYPE_MESSAGE ? mapEntries === null || mapEntries === void 0 ? void 0 : mapEntries.get(trimLeadingDot(proto.typeName)) : void 0;
    if (mapEntry) {
      field.fieldKind = "map";
      const { key, value } = findMapEntryFields(mapEntry);
      field.mapKey = key.scalar;
      field.mapKind = value.fieldKind;
      field.message = value.message;
      field.delimitedEncoding = false;
      field.enum = value.enum;
      field.scalar = value.scalar;
      return field;
    }
    field.fieldKind = "list";
    switch (type) {
      case TYPE_MESSAGE:
      case TYPE_GROUP:
        field.listKind = "message";
        field.message = reg.getMessage(trimLeadingDot(proto.typeName));
        assert(field.message);
        field.delimitedEncoding = isDelimitedEncoding(proto, parentOrFile);
        break;
      case TYPE_ENUM:
        field.listKind = "enum";
        field.enum = reg.getEnum(trimLeadingDot(proto.typeName));
        assert(field.enum);
        break;
      default:
        field.listKind = "scalar";
        field.scalar = type;
        field.longAsString = jstype == JS_STRING;
        break;
    }
    field.packed = isPackedField(proto, parentOrFile);
    return field;
  }
  switch (type) {
    case TYPE_MESSAGE:
    case TYPE_GROUP:
      field.fieldKind = "message";
      field.message = reg.getMessage(trimLeadingDot(proto.typeName));
      assert(field.message, `invalid FieldDescriptorProto: type_name ${proto.typeName} not found`);
      field.delimitedEncoding = isDelimitedEncoding(proto, parentOrFile);
      field.getDefaultValue = () => void 0;
      break;
    case TYPE_ENUM: {
      const enumeration = reg.getEnum(trimLeadingDot(proto.typeName));
      assert(enumeration !== void 0, `invalid FieldDescriptorProto: type_name ${proto.typeName} not found`);
      field.fieldKind = "enum";
      field.enum = reg.getEnum(trimLeadingDot(proto.typeName));
      field.getDefaultValue = () => {
        return unsafeIsSetExplicit(proto, "defaultValue") ? parseTextFormatEnumValue(enumeration, proto.defaultValue) : void 0;
      };
      break;
    }
    default: {
      field.fieldKind = "scalar";
      field.scalar = type;
      field.longAsString = jstype == JS_STRING;
      field.getDefaultValue = () => {
        return unsafeIsSetExplicit(proto, "defaultValue") ? parseTextFormatScalarValue(type, proto.defaultValue) : void 0;
      };
      break;
    }
  }
  return field;
}
function getFileEdition(proto) {
  switch (proto.syntax) {
    case "":
    case "proto2":
      return EDITION_PROTO22;
    case "proto3":
      return EDITION_PROTO32;
    case "editions":
      if (proto.edition === EDITION_UNSTABLE) {
        return maximumEdition;
      }
      if (proto.edition in featureDefaults) {
        return proto.edition;
      }
      throw new Error(`${proto.name}: unsupported edition`);
    default:
      throw new Error(`${proto.name}: unsupported syntax "${proto.syntax}"`);
  }
}
function findFileDependencies(proto, reg) {
  return proto.dependency.map((wantName) => {
    const dep = reg.getFile(wantName);
    if (!dep) {
      throw new Error(`Cannot find ${wantName}, imported by ${proto.name}`);
    }
    return dep;
  });
}
function findEnumSharedPrefix(enumName, values) {
  const prefix = camelToSnakeCase(enumName) + "_";
  for (const value of values) {
    if (!value.name.toLowerCase().startsWith(prefix)) {
      return void 0;
    }
    const shortName = value.name.substring(prefix.length);
    if (shortName.length == 0) {
      return void 0;
    }
    if (/^\d/.test(shortName)) {
      return void 0;
    }
  }
  return prefix;
}
function camelToSnakeCase(camel) {
  return (camel.substring(0, 1) + camel.substring(1).replace(/[A-Z]/g, (c) => "_" + c)).toLowerCase();
}
function makeTypeName(proto, parent, file) {
  let typeName;
  if (parent) {
    typeName = `${parent.typeName}.${proto.name}`;
  } else if (file.proto.package.length > 0) {
    typeName = `${file.proto.package}.${proto.name}`;
  } else {
    typeName = `${proto.name}`;
  }
  return typeName;
}
function trimLeadingDot(typeName) {
  return typeName.startsWith(".") ? typeName.substring(1) : typeName;
}
function findOneof(proto, allOneofs) {
  if (!unsafeIsSetExplicit(proto, "oneofIndex")) {
    return void 0;
  }
  if (proto.proto3Optional) {
    return void 0;
  }
  const oneof = allOneofs[proto.oneofIndex];
  assert(oneof, `invalid FieldDescriptorProto: oneof #${proto.oneofIndex} for field #${proto.number} not found`);
  return oneof;
}
function getFieldPresence(proto, oneof, isExtension, parent) {
  if (proto.label == LABEL_REQUIRED) {
    return LEGACY_REQUIRED;
  }
  if (proto.label == LABEL_REPEATED) {
    return IMPLICIT3;
  }
  if (!!oneof || proto.proto3Optional) {
    return EXPLICIT;
  }
  if (isExtension) {
    return EXPLICIT;
  }
  const resolved = resolveFeature("fieldPresence", { proto, parent });
  if (resolved == IMPLICIT3 && (proto.type == TYPE_MESSAGE || proto.type == TYPE_GROUP)) {
    return EXPLICIT;
  }
  return resolved;
}
function isPackedField(proto, parent) {
  if (proto.label != LABEL_REPEATED) {
    return false;
  }
  switch (proto.type) {
    case TYPE_STRING:
    case TYPE_BYTES:
    case TYPE_GROUP:
    case TYPE_MESSAGE:
      return false;
  }
  const o = proto.options;
  if (o && unsafeIsSetExplicit(o, "packed")) {
    return o.packed;
  }
  return PACKED == resolveFeature("repeatedFieldEncoding", {
    proto,
    parent
  });
}
function findMapEntryFields(mapEntry) {
  const key = mapEntry.fields.find((f) => f.number === 1);
  const value = mapEntry.fields.find((f) => f.number === 2);
  assert(key && key.fieldKind == "scalar" && key.scalar != ScalarType.BYTES && key.scalar != ScalarType.FLOAT && key.scalar != ScalarType.DOUBLE && value && value.fieldKind != "list" && value.fieldKind != "map");
  return { key, value };
}
function isEnumOpen(desc) {
  var _a2;
  return OPEN == resolveFeature("enumType", {
    proto: desc.proto,
    parent: (_a2 = desc.parent) !== null && _a2 !== void 0 ? _a2 : desc.file
  });
}
function isDelimitedEncoding(proto, parent) {
  if (proto.type == TYPE_GROUP) {
    return true;
  }
  return DELIMITED == resolveFeature("messageEncoding", {
    proto,
    parent
  });
}
function isUtf8Validated(proto, parent) {
  return VERIFY == resolveFeature("utf8Validation", {
    proto,
    parent
  });
}
function resolveFeature(name, ref) {
  var _a2, _b;
  const featureSet = (_a2 = ref.proto.options) === null || _a2 === void 0 ? void 0 : _a2.features;
  if (featureSet) {
    const val = featureSet[name];
    if (val != 0) {
      return val;
    }
  }
  if ("kind" in ref) {
    if (ref.kind == "message") {
      return resolveFeature(name, (_b = ref.parent) !== null && _b !== void 0 ? _b : ref.file);
    }
    const editionDefaults = featureDefaults[ref.edition];
    if (!editionDefaults) {
      throw new Error(`feature default for edition ${ref.edition} not found`);
    }
    return editionDefaults[name];
  }
  return resolveFeature(name, ref.parent);
}
function assert(condition, msg) {
  if (!condition) {
    throw new Error(msg);
  }
}

// node_modules/.pnpm/@bufbuild+protobuf@2.12.1/node_modules/@bufbuild/protobuf/dist/esm/codegenv2/boot.js
function boot(boot2) {
  const root2 = bootFileDescriptorProto(boot2);
  root2.messageType.forEach(restoreJsonNames);
  const reg = createFileRegistry(root2, () => void 0);
  return reg.getFile(root2.name);
}
function bootFileDescriptorProto(init) {
  const proto = /* @__PURE__ */ Object.create({
    syntax: "",
    edition: 0
  });
  return Object.assign(proto, Object.assign(Object.assign({ $typeName: "google.protobuf.FileDescriptorProto", dependency: [], publicDependency: [], weakDependency: [], optionDependency: [], service: [], extension: [] }, init), { messageType: init.messageType.map(bootDescriptorProto), enumType: init.enumType.map(bootEnumDescriptorProto) }));
}
function bootDescriptorProto(init) {
  var _a2, _b, _c, _d, _e, _f, _g, _h;
  const proto = /* @__PURE__ */ Object.create({
    visibility: 0
  });
  return Object.assign(proto, {
    $typeName: "google.protobuf.DescriptorProto",
    name: init.name,
    field: (_b = (_a2 = init.field) === null || _a2 === void 0 ? void 0 : _a2.map(bootFieldDescriptorProto)) !== null && _b !== void 0 ? _b : [],
    extension: [],
    nestedType: (_d = (_c = init.nestedType) === null || _c === void 0 ? void 0 : _c.map(bootDescriptorProto)) !== null && _d !== void 0 ? _d : [],
    enumType: (_f = (_e = init.enumType) === null || _e === void 0 ? void 0 : _e.map(bootEnumDescriptorProto)) !== null && _f !== void 0 ? _f : [],
    extensionRange: (_h = (_g = init.extensionRange) === null || _g === void 0 ? void 0 : _g.map((e) => Object.assign({ $typeName: "google.protobuf.DescriptorProto.ExtensionRange" }, e))) !== null && _h !== void 0 ? _h : [],
    oneofDecl: [],
    reservedRange: [],
    reservedName: []
  });
}
function bootFieldDescriptorProto(init) {
  const proto = /* @__PURE__ */ Object.create({
    label: 1,
    typeName: "",
    extendee: "",
    defaultValue: "",
    oneofIndex: 0,
    jsonName: "",
    proto3Optional: false
  });
  return Object.assign(proto, Object.assign(Object.assign({ $typeName: "google.protobuf.FieldDescriptorProto" }, init), { options: init.options ? bootFieldOptions(init.options) : void 0 }));
}
function bootFieldOptions(init) {
  var _a2, _b, _c;
  const proto = /* @__PURE__ */ Object.create({
    ctype: 0,
    packed: false,
    jstype: 0,
    lazy: false,
    unverifiedLazy: false,
    deprecated: false,
    weak: false,
    debugRedact: false,
    retention: 0
  });
  return Object.assign(proto, Object.assign(Object.assign({ $typeName: "google.protobuf.FieldOptions" }, init), { targets: (_a2 = init.targets) !== null && _a2 !== void 0 ? _a2 : [], editionDefaults: (_c = (_b = init.editionDefaults) === null || _b === void 0 ? void 0 : _b.map((e) => Object.assign({ $typeName: "google.protobuf.FieldOptions.EditionDefault" }, e))) !== null && _c !== void 0 ? _c : [], uninterpretedOption: [] }));
}
function bootEnumDescriptorProto(init) {
  const proto = /* @__PURE__ */ Object.create({
    visibility: 0
  });
  return Object.assign(proto, {
    $typeName: "google.protobuf.EnumDescriptorProto",
    name: init.name,
    reservedName: [],
    reservedRange: [],
    value: init.value.map((e) => Object.assign({ $typeName: "google.protobuf.EnumValueDescriptorProto" }, e))
  });
}

// node_modules/.pnpm/@bufbuild+protobuf@2.12.1/node_modules/@bufbuild/protobuf/dist/esm/codegenv2/message.js
function messageDesc(file, path2, ...paths) {
  return paths.reduce((acc, cur) => acc.nestedMessages[cur], file.messages[path2]);
}

// node_modules/.pnpm/@bufbuild+protobuf@2.12.1/node_modules/@bufbuild/protobuf/dist/esm/wkt/gen/google/protobuf/descriptor_pb.js
var file_google_protobuf_descriptor = /* @__PURE__ */ boot({ "name": "google/protobuf/descriptor.proto", "package": "google.protobuf", "messageType": [{ "name": "FileDescriptorSet", "field": [{ "name": "file", "number": 1, "type": 11, "label": 3, "typeName": ".google.protobuf.FileDescriptorProto" }], "extensionRange": [{ "start": 536e6, "end": 536000001 }] }, { "name": "FileDescriptorProto", "field": [{ "name": "name", "number": 1, "type": 9, "label": 1 }, { "name": "package", "number": 2, "type": 9, "label": 1 }, { "name": "dependency", "number": 3, "type": 9, "label": 3 }, { "name": "public_dependency", "number": 10, "type": 5, "label": 3 }, { "name": "weak_dependency", "number": 11, "type": 5, "label": 3 }, { "name": "option_dependency", "number": 15, "type": 9, "label": 3 }, { "name": "message_type", "number": 4, "type": 11, "label": 3, "typeName": ".google.protobuf.DescriptorProto" }, { "name": "enum_type", "number": 5, "type": 11, "label": 3, "typeName": ".google.protobuf.EnumDescriptorProto" }, { "name": "service", "number": 6, "type": 11, "label": 3, "typeName": ".google.protobuf.ServiceDescriptorProto" }, { "name": "extension", "number": 7, "type": 11, "label": 3, "typeName": ".google.protobuf.FieldDescriptorProto" }, { "name": "options", "number": 8, "type": 11, "label": 1, "typeName": ".google.protobuf.FileOptions" }, { "name": "source_code_info", "number": 9, "type": 11, "label": 1, "typeName": ".google.protobuf.SourceCodeInfo" }, { "name": "syntax", "number": 12, "type": 9, "label": 1 }, { "name": "edition", "number": 14, "type": 14, "label": 1, "typeName": ".google.protobuf.Edition" }] }, { "name": "DescriptorProto", "field": [{ "name": "name", "number": 1, "type": 9, "label": 1 }, { "name": "field", "number": 2, "type": 11, "label": 3, "typeName": ".google.protobuf.FieldDescriptorProto" }, { "name": "extension", "number": 6, "type": 11, "label": 3, "typeName": ".google.protobuf.FieldDescriptorProto" }, { "name": "nested_type", "number": 3, "type": 11, "label": 3, "typeName": ".google.protobuf.DescriptorProto" }, { "name": "enum_type", "number": 4, "type": 11, "label": 3, "typeName": ".google.protobuf.EnumDescriptorProto" }, { "name": "extension_range", "number": 5, "type": 11, "label": 3, "typeName": ".google.protobuf.DescriptorProto.ExtensionRange" }, { "name": "oneof_decl", "number": 8, "type": 11, "label": 3, "typeName": ".google.protobuf.OneofDescriptorProto" }, { "name": "options", "number": 7, "type": 11, "label": 1, "typeName": ".google.protobuf.MessageOptions" }, { "name": "reserved_range", "number": 9, "type": 11, "label": 3, "typeName": ".google.protobuf.DescriptorProto.ReservedRange" }, { "name": "reserved_name", "number": 10, "type": 9, "label": 3 }, { "name": "visibility", "number": 11, "type": 14, "label": 1, "typeName": ".google.protobuf.SymbolVisibility" }], "nestedType": [{ "name": "ExtensionRange", "field": [{ "name": "start", "number": 1, "type": 5, "label": 1 }, { "name": "end", "number": 2, "type": 5, "label": 1 }, { "name": "options", "number": 3, "type": 11, "label": 1, "typeName": ".google.protobuf.ExtensionRangeOptions" }] }, { "name": "ReservedRange", "field": [{ "name": "start", "number": 1, "type": 5, "label": 1 }, { "name": "end", "number": 2, "type": 5, "label": 1 }] }] }, { "name": "ExtensionRangeOptions", "field": [{ "name": "uninterpreted_option", "number": 999, "type": 11, "label": 3, "typeName": ".google.protobuf.UninterpretedOption" }, { "name": "declaration", "number": 2, "type": 11, "label": 3, "typeName": ".google.protobuf.ExtensionRangeOptions.Declaration", "options": { "retention": 2 } }, { "name": "features", "number": 50, "type": 11, "label": 1, "typeName": ".google.protobuf.FeatureSet" }, { "name": "verification", "number": 3, "type": 14, "label": 1, "typeName": ".google.protobuf.ExtensionRangeOptions.VerificationState", "defaultValue": "UNVERIFIED", "options": { "retention": 2 } }], "nestedType": [{ "name": "Declaration", "field": [{ "name": "number", "number": 1, "type": 5, "label": 1 }, { "name": "full_name", "number": 2, "type": 9, "label": 1 }, { "name": "type", "number": 3, "type": 9, "label": 1 }, { "name": "reserved", "number": 5, "type": 8, "label": 1 }, { "name": "repeated", "number": 6, "type": 8, "label": 1 }] }], "enumType": [{ "name": "VerificationState", "value": [{ "name": "DECLARATION", "number": 0 }, { "name": "UNVERIFIED", "number": 1 }] }], "extensionRange": [{ "start": 1e3, "end": 536870912 }] }, { "name": "FieldDescriptorProto", "field": [{ "name": "name", "number": 1, "type": 9, "label": 1 }, { "name": "number", "number": 3, "type": 5, "label": 1 }, { "name": "label", "number": 4, "type": 14, "label": 1, "typeName": ".google.protobuf.FieldDescriptorProto.Label" }, { "name": "type", "number": 5, "type": 14, "label": 1, "typeName": ".google.protobuf.FieldDescriptorProto.Type" }, { "name": "type_name", "number": 6, "type": 9, "label": 1 }, { "name": "extendee", "number": 2, "type": 9, "label": 1 }, { "name": "default_value", "number": 7, "type": 9, "label": 1 }, { "name": "oneof_index", "number": 9, "type": 5, "label": 1 }, { "name": "json_name", "number": 10, "type": 9, "label": 1 }, { "name": "options", "number": 8, "type": 11, "label": 1, "typeName": ".google.protobuf.FieldOptions" }, { "name": "proto3_optional", "number": 17, "type": 8, "label": 1 }], "enumType": [{ "name": "Type", "value": [{ "name": "TYPE_DOUBLE", "number": 1 }, { "name": "TYPE_FLOAT", "number": 2 }, { "name": "TYPE_INT64", "number": 3 }, { "name": "TYPE_UINT64", "number": 4 }, { "name": "TYPE_INT32", "number": 5 }, { "name": "TYPE_FIXED64", "number": 6 }, { "name": "TYPE_FIXED32", "number": 7 }, { "name": "TYPE_BOOL", "number": 8 }, { "name": "TYPE_STRING", "number": 9 }, { "name": "TYPE_GROUP", "number": 10 }, { "name": "TYPE_MESSAGE", "number": 11 }, { "name": "TYPE_BYTES", "number": 12 }, { "name": "TYPE_UINT32", "number": 13 }, { "name": "TYPE_ENUM", "number": 14 }, { "name": "TYPE_SFIXED32", "number": 15 }, { "name": "TYPE_SFIXED64", "number": 16 }, { "name": "TYPE_SINT32", "number": 17 }, { "name": "TYPE_SINT64", "number": 18 }] }, { "name": "Label", "value": [{ "name": "LABEL_OPTIONAL", "number": 1 }, { "name": "LABEL_REPEATED", "number": 3 }, { "name": "LABEL_REQUIRED", "number": 2 }] }] }, { "name": "OneofDescriptorProto", "field": [{ "name": "name", "number": 1, "type": 9, "label": 1 }, { "name": "options", "number": 2, "type": 11, "label": 1, "typeName": ".google.protobuf.OneofOptions" }] }, { "name": "EnumDescriptorProto", "field": [{ "name": "name", "number": 1, "type": 9, "label": 1 }, { "name": "value", "number": 2, "type": 11, "label": 3, "typeName": ".google.protobuf.EnumValueDescriptorProto" }, { "name": "options", "number": 3, "type": 11, "label": 1, "typeName": ".google.protobuf.EnumOptions" }, { "name": "reserved_range", "number": 4, "type": 11, "label": 3, "typeName": ".google.protobuf.EnumDescriptorProto.EnumReservedRange" }, { "name": "reserved_name", "number": 5, "type": 9, "label": 3 }, { "name": "visibility", "number": 6, "type": 14, "label": 1, "typeName": ".google.protobuf.SymbolVisibility" }], "nestedType": [{ "name": "EnumReservedRange", "field": [{ "name": "start", "number": 1, "type": 5, "label": 1 }, { "name": "end", "number": 2, "type": 5, "label": 1 }] }] }, { "name": "EnumValueDescriptorProto", "field": [{ "name": "name", "number": 1, "type": 9, "label": 1 }, { "name": "number", "number": 2, "type": 5, "label": 1 }, { "name": "options", "number": 3, "type": 11, "label": 1, "typeName": ".google.protobuf.EnumValueOptions" }] }, { "name": "ServiceDescriptorProto", "field": [{ "name": "name", "number": 1, "type": 9, "label": 1 }, { "name": "method", "number": 2, "type": 11, "label": 3, "typeName": ".google.protobuf.MethodDescriptorProto" }, { "name": "options", "number": 3, "type": 11, "label": 1, "typeName": ".google.protobuf.ServiceOptions" }] }, { "name": "MethodDescriptorProto", "field": [{ "name": "name", "number": 1, "type": 9, "label": 1 }, { "name": "input_type", "number": 2, "type": 9, "label": 1 }, { "name": "output_type", "number": 3, "type": 9, "label": 1 }, { "name": "options", "number": 4, "type": 11, "label": 1, "typeName": ".google.protobuf.MethodOptions" }, { "name": "client_streaming", "number": 5, "type": 8, "label": 1, "defaultValue": "false" }, { "name": "server_streaming", "number": 6, "type": 8, "label": 1, "defaultValue": "false" }] }, { "name": "FileOptions", "field": [{ "name": "java_package", "number": 1, "type": 9, "label": 1 }, { "name": "java_outer_classname", "number": 8, "type": 9, "label": 1 }, { "name": "java_multiple_files", "number": 10, "type": 8, "label": 1, "defaultValue": "false", "options": {} }, { "name": "java_generate_equals_and_hash", "number": 20, "type": 8, "label": 1, "options": { "deprecated": true } }, { "name": "java_string_check_utf8", "number": 27, "type": 8, "label": 1, "defaultValue": "false" }, { "name": "optimize_for", "number": 9, "type": 14, "label": 1, "typeName": ".google.protobuf.FileOptions.OptimizeMode", "defaultValue": "SPEED" }, { "name": "go_package", "number": 11, "type": 9, "label": 1 }, { "name": "cc_generic_services", "number": 16, "type": 8, "label": 1, "defaultValue": "false" }, { "name": "java_generic_services", "number": 17, "type": 8, "label": 1, "defaultValue": "false" }, { "name": "py_generic_services", "number": 18, "type": 8, "label": 1, "defaultValue": "false" }, { "name": "deprecated", "number": 23, "type": 8, "label": 1, "defaultValue": "false" }, { "name": "cc_enable_arenas", "number": 31, "type": 8, "label": 1, "defaultValue": "true" }, { "name": "objc_class_prefix", "number": 36, "type": 9, "label": 1 }, { "name": "csharp_namespace", "number": 37, "type": 9, "label": 1 }, { "name": "swift_prefix", "number": 39, "type": 9, "label": 1 }, { "name": "php_class_prefix", "number": 40, "type": 9, "label": 1 }, { "name": "php_namespace", "number": 41, "type": 9, "label": 1 }, { "name": "php_metadata_namespace", "number": 44, "type": 9, "label": 1 }, { "name": "ruby_package", "number": 45, "type": 9, "label": 1 }, { "name": "features", "number": 50, "type": 11, "label": 1, "typeName": ".google.protobuf.FeatureSet" }, { "name": "uninterpreted_option", "number": 999, "type": 11, "label": 3, "typeName": ".google.protobuf.UninterpretedOption" }], "enumType": [{ "name": "OptimizeMode", "value": [{ "name": "SPEED", "number": 1 }, { "name": "CODE_SIZE", "number": 2 }, { "name": "LITE_RUNTIME", "number": 3 }] }], "extensionRange": [{ "start": 1e3, "end": 536870912 }] }, { "name": "MessageOptions", "field": [{ "name": "message_set_wire_format", "number": 1, "type": 8, "label": 1, "defaultValue": "false" }, { "name": "no_standard_descriptor_accessor", "number": 2, "type": 8, "label": 1, "defaultValue": "false" }, { "name": "deprecated", "number": 3, "type": 8, "label": 1, "defaultValue": "false" }, { "name": "map_entry", "number": 7, "type": 8, "label": 1 }, { "name": "deprecated_legacy_json_field_conflicts", "number": 11, "type": 8, "label": 1, "options": { "deprecated": true } }, { "name": "features", "number": 12, "type": 11, "label": 1, "typeName": ".google.protobuf.FeatureSet" }, { "name": "uninterpreted_option", "number": 999, "type": 11, "label": 3, "typeName": ".google.protobuf.UninterpretedOption" }], "extensionRange": [{ "start": 1e3, "end": 536870912 }] }, { "name": "FieldOptions", "field": [{ "name": "ctype", "number": 1, "type": 14, "label": 1, "typeName": ".google.protobuf.FieldOptions.CType", "defaultValue": "STRING" }, { "name": "packed", "number": 2, "type": 8, "label": 1 }, { "name": "jstype", "number": 6, "type": 14, "label": 1, "typeName": ".google.protobuf.FieldOptions.JSType", "defaultValue": "JS_NORMAL" }, { "name": "lazy", "number": 5, "type": 8, "label": 1, "defaultValue": "false" }, { "name": "unverified_lazy", "number": 15, "type": 8, "label": 1, "defaultValue": "false" }, { "name": "deprecated", "number": 3, "type": 8, "label": 1, "defaultValue": "false" }, { "name": "weak", "number": 10, "type": 8, "label": 1, "defaultValue": "false", "options": { "deprecated": true } }, { "name": "debug_redact", "number": 16, "type": 8, "label": 1, "defaultValue": "false" }, { "name": "retention", "number": 17, "type": 14, "label": 1, "typeName": ".google.protobuf.FieldOptions.OptionRetention" }, { "name": "targets", "number": 19, "type": 14, "label": 3, "typeName": ".google.protobuf.FieldOptions.OptionTargetType" }, { "name": "edition_defaults", "number": 20, "type": 11, "label": 3, "typeName": ".google.protobuf.FieldOptions.EditionDefault" }, { "name": "features", "number": 21, "type": 11, "label": 1, "typeName": ".google.protobuf.FeatureSet" }, { "name": "feature_support", "number": 22, "type": 11, "label": 1, "typeName": ".google.protobuf.FieldOptions.FeatureSupport" }, { "name": "uninterpreted_option", "number": 999, "type": 11, "label": 3, "typeName": ".google.protobuf.UninterpretedOption" }], "nestedType": [{ "name": "EditionDefault", "field": [{ "name": "edition", "number": 3, "type": 14, "label": 1, "typeName": ".google.protobuf.Edition" }, { "name": "value", "number": 2, "type": 9, "label": 1 }] }, { "name": "FeatureSupport", "field": [{ "name": "edition_introduced", "number": 1, "type": 14, "label": 1, "typeName": ".google.protobuf.Edition" }, { "name": "edition_deprecated", "number": 2, "type": 14, "label": 1, "typeName": ".google.protobuf.Edition" }, { "name": "deprecation_warning", "number": 3, "type": 9, "label": 1 }, { "name": "edition_removed", "number": 4, "type": 14, "label": 1, "typeName": ".google.protobuf.Edition" }, { "name": "removal_error", "number": 5, "type": 9, "label": 1 }] }], "enumType": [{ "name": "CType", "value": [{ "name": "STRING", "number": 0 }, { "name": "CORD", "number": 1 }, { "name": "STRING_PIECE", "number": 2 }] }, { "name": "JSType", "value": [{ "name": "JS_NORMAL", "number": 0 }, { "name": "JS_STRING", "number": 1 }, { "name": "JS_NUMBER", "number": 2 }] }, { "name": "OptionRetention", "value": [{ "name": "RETENTION_UNKNOWN", "number": 0 }, { "name": "RETENTION_RUNTIME", "number": 1 }, { "name": "RETENTION_SOURCE", "number": 2 }] }, { "name": "OptionTargetType", "value": [{ "name": "TARGET_TYPE_UNKNOWN", "number": 0 }, { "name": "TARGET_TYPE_FILE", "number": 1 }, { "name": "TARGET_TYPE_EXTENSION_RANGE", "number": 2 }, { "name": "TARGET_TYPE_MESSAGE", "number": 3 }, { "name": "TARGET_TYPE_FIELD", "number": 4 }, { "name": "TARGET_TYPE_ONEOF", "number": 5 }, { "name": "TARGET_TYPE_ENUM", "number": 6 }, { "name": "TARGET_TYPE_ENUM_ENTRY", "number": 7 }, { "name": "TARGET_TYPE_SERVICE", "number": 8 }, { "name": "TARGET_TYPE_METHOD", "number": 9 }] }], "extensionRange": [{ "start": 1e3, "end": 536870912 }] }, { "name": "OneofOptions", "field": [{ "name": "features", "number": 1, "type": 11, "label": 1, "typeName": ".google.protobuf.FeatureSet" }, { "name": "uninterpreted_option", "number": 999, "type": 11, "label": 3, "typeName": ".google.protobuf.UninterpretedOption" }], "extensionRange": [{ "start": 1e3, "end": 536870912 }] }, { "name": "EnumOptions", "field": [{ "name": "allow_alias", "number": 2, "type": 8, "label": 1 }, { "name": "deprecated", "number": 3, "type": 8, "label": 1, "defaultValue": "false" }, { "name": "deprecated_legacy_json_field_conflicts", "number": 6, "type": 8, "label": 1, "options": { "deprecated": true } }, { "name": "features", "number": 7, "type": 11, "label": 1, "typeName": ".google.protobuf.FeatureSet" }, { "name": "uninterpreted_option", "number": 999, "type": 11, "label": 3, "typeName": ".google.protobuf.UninterpretedOption" }], "extensionRange": [{ "start": 1e3, "end": 536870912 }] }, { "name": "EnumValueOptions", "field": [{ "name": "deprecated", "number": 1, "type": 8, "label": 1, "defaultValue": "false" }, { "name": "features", "number": 2, "type": 11, "label": 1, "typeName": ".google.protobuf.FeatureSet" }, { "name": "debug_redact", "number": 3, "type": 8, "label": 1, "defaultValue": "false" }, { "name": "feature_support", "number": 4, "type": 11, "label": 1, "typeName": ".google.protobuf.FieldOptions.FeatureSupport" }, { "name": "uninterpreted_option", "number": 999, "type": 11, "label": 3, "typeName": ".google.protobuf.UninterpretedOption" }], "extensionRange": [{ "start": 1e3, "end": 536870912 }] }, { "name": "ServiceOptions", "field": [{ "name": "features", "number": 34, "type": 11, "label": 1, "typeName": ".google.protobuf.FeatureSet" }, { "name": "deprecated", "number": 33, "type": 8, "label": 1, "defaultValue": "false" }, { "name": "uninterpreted_option", "number": 999, "type": 11, "label": 3, "typeName": ".google.protobuf.UninterpretedOption" }], "extensionRange": [{ "start": 1e3, "end": 536870912 }] }, { "name": "MethodOptions", "field": [{ "name": "deprecated", "number": 33, "type": 8, "label": 1, "defaultValue": "false" }, { "name": "idempotency_level", "number": 34, "type": 14, "label": 1, "typeName": ".google.protobuf.MethodOptions.IdempotencyLevel", "defaultValue": "IDEMPOTENCY_UNKNOWN" }, { "name": "features", "number": 35, "type": 11, "label": 1, "typeName": ".google.protobuf.FeatureSet" }, { "name": "uninterpreted_option", "number": 999, "type": 11, "label": 3, "typeName": ".google.protobuf.UninterpretedOption" }], "enumType": [{ "name": "IdempotencyLevel", "value": [{ "name": "IDEMPOTENCY_UNKNOWN", "number": 0 }, { "name": "NO_SIDE_EFFECTS", "number": 1 }, { "name": "IDEMPOTENT", "number": 2 }] }], "extensionRange": [{ "start": 1e3, "end": 536870912 }] }, { "name": "UninterpretedOption", "field": [{ "name": "name", "number": 2, "type": 11, "label": 3, "typeName": ".google.protobuf.UninterpretedOption.NamePart" }, { "name": "identifier_value", "number": 3, "type": 9, "label": 1 }, { "name": "positive_int_value", "number": 4, "type": 4, "label": 1 }, { "name": "negative_int_value", "number": 5, "type": 3, "label": 1 }, { "name": "double_value", "number": 6, "type": 1, "label": 1 }, { "name": "string_value", "number": 7, "type": 12, "label": 1 }, { "name": "aggregate_value", "number": 8, "type": 9, "label": 1 }], "nestedType": [{ "name": "NamePart", "field": [{ "name": "name_part", "number": 1, "type": 9, "label": 2 }, { "name": "is_extension", "number": 2, "type": 8, "label": 2 }] }] }, { "name": "FeatureSet", "field": [{ "name": "field_presence", "number": 1, "type": 14, "label": 1, "typeName": ".google.protobuf.FeatureSet.FieldPresence", "options": { "retention": 1, "targets": [4, 1], "editionDefaults": [{ "value": "EXPLICIT", "edition": 900 }, { "value": "IMPLICIT", "edition": 999 }, { "value": "EXPLICIT", "edition": 1e3 }] } }, { "name": "enum_type", "number": 2, "type": 14, "label": 1, "typeName": ".google.protobuf.FeatureSet.EnumType", "options": { "retention": 1, "targets": [6, 1], "editionDefaults": [{ "value": "CLOSED", "edition": 900 }, { "value": "OPEN", "edition": 999 }] } }, { "name": "repeated_field_encoding", "number": 3, "type": 14, "label": 1, "typeName": ".google.protobuf.FeatureSet.RepeatedFieldEncoding", "options": { "retention": 1, "targets": [4, 1], "editionDefaults": [{ "value": "EXPANDED", "edition": 900 }, { "value": "PACKED", "edition": 999 }] } }, { "name": "utf8_validation", "number": 4, "type": 14, "label": 1, "typeName": ".google.protobuf.FeatureSet.Utf8Validation", "options": { "retention": 1, "targets": [4, 1], "editionDefaults": [{ "value": "NONE", "edition": 900 }, { "value": "VERIFY", "edition": 999 }] } }, { "name": "message_encoding", "number": 5, "type": 14, "label": 1, "typeName": ".google.protobuf.FeatureSet.MessageEncoding", "options": { "retention": 1, "targets": [4, 1], "editionDefaults": [{ "value": "LENGTH_PREFIXED", "edition": 900 }] } }, { "name": "json_format", "number": 6, "type": 14, "label": 1, "typeName": ".google.protobuf.FeatureSet.JsonFormat", "options": { "retention": 1, "targets": [3, 6, 1], "editionDefaults": [{ "value": "LEGACY_BEST_EFFORT", "edition": 900 }, { "value": "ALLOW", "edition": 999 }] } }, { "name": "enforce_naming_style", "number": 7, "type": 14, "label": 1, "typeName": ".google.protobuf.FeatureSet.EnforceNamingStyle", "options": { "retention": 2, "targets": [1, 2, 3, 4, 5, 6, 7, 8, 9], "editionDefaults": [{ "value": "STYLE_LEGACY", "edition": 900 }, { "value": "STYLE2024", "edition": 1001 }] } }, { "name": "default_symbol_visibility", "number": 8, "type": 14, "label": 1, "typeName": ".google.protobuf.FeatureSet.VisibilityFeature.DefaultSymbolVisibility", "options": { "retention": 2, "targets": [1], "editionDefaults": [{ "value": "EXPORT_ALL", "edition": 900 }, { "value": "EXPORT_TOP_LEVEL", "edition": 1001 }] } }], "nestedType": [{ "name": "VisibilityFeature", "enumType": [{ "name": "DefaultSymbolVisibility", "value": [{ "name": "DEFAULT_SYMBOL_VISIBILITY_UNKNOWN", "number": 0 }, { "name": "EXPORT_ALL", "number": 1 }, { "name": "EXPORT_TOP_LEVEL", "number": 2 }, { "name": "LOCAL_ALL", "number": 3 }, { "name": "STRICT", "number": 4 }] }] }], "enumType": [{ "name": "FieldPresence", "value": [{ "name": "FIELD_PRESENCE_UNKNOWN", "number": 0 }, { "name": "EXPLICIT", "number": 1 }, { "name": "IMPLICIT", "number": 2 }, { "name": "LEGACY_REQUIRED", "number": 3 }] }, { "name": "EnumType", "value": [{ "name": "ENUM_TYPE_UNKNOWN", "number": 0 }, { "name": "OPEN", "number": 1 }, { "name": "CLOSED", "number": 2 }] }, { "name": "RepeatedFieldEncoding", "value": [{ "name": "REPEATED_FIELD_ENCODING_UNKNOWN", "number": 0 }, { "name": "PACKED", "number": 1 }, { "name": "EXPANDED", "number": 2 }] }, { "name": "Utf8Validation", "value": [{ "name": "UTF8_VALIDATION_UNKNOWN", "number": 0 }, { "name": "VERIFY", "number": 2 }, { "name": "NONE", "number": 3 }] }, { "name": "MessageEncoding", "value": [{ "name": "MESSAGE_ENCODING_UNKNOWN", "number": 0 }, { "name": "LENGTH_PREFIXED", "number": 1 }, { "name": "DELIMITED", "number": 2 }] }, { "name": "JsonFormat", "value": [{ "name": "JSON_FORMAT_UNKNOWN", "number": 0 }, { "name": "ALLOW", "number": 1 }, { "name": "LEGACY_BEST_EFFORT", "number": 2 }] }, { "name": "EnforceNamingStyle", "value": [{ "name": "ENFORCE_NAMING_STYLE_UNKNOWN", "number": 0 }, { "name": "STYLE2024", "number": 1 }, { "name": "STYLE_LEGACY", "number": 2 }] }], "extensionRange": [{ "start": 1e3, "end": 9995 }, { "start": 9995, "end": 1e4 }, { "start": 1e4, "end": 10001 }] }, { "name": "FeatureSetDefaults", "field": [{ "name": "defaults", "number": 1, "type": 11, "label": 3, "typeName": ".google.protobuf.FeatureSetDefaults.FeatureSetEditionDefault" }, { "name": "minimum_edition", "number": 4, "type": 14, "label": 1, "typeName": ".google.protobuf.Edition" }, { "name": "maximum_edition", "number": 5, "type": 14, "label": 1, "typeName": ".google.protobuf.Edition" }], "nestedType": [{ "name": "FeatureSetEditionDefault", "field": [{ "name": "edition", "number": 3, "type": 14, "label": 1, "typeName": ".google.protobuf.Edition" }, { "name": "overridable_features", "number": 4, "type": 11, "label": 1, "typeName": ".google.protobuf.FeatureSet" }, { "name": "fixed_features", "number": 5, "type": 11, "label": 1, "typeName": ".google.protobuf.FeatureSet" }] }] }, { "name": "SourceCodeInfo", "field": [{ "name": "location", "number": 1, "type": 11, "label": 3, "typeName": ".google.protobuf.SourceCodeInfo.Location" }], "nestedType": [{ "name": "Location", "field": [{ "name": "path", "number": 1, "type": 5, "label": 3, "options": { "packed": true } }, { "name": "span", "number": 2, "type": 5, "label": 3, "options": { "packed": true } }, { "name": "leading_comments", "number": 3, "type": 9, "label": 1 }, { "name": "trailing_comments", "number": 4, "type": 9, "label": 1 }, { "name": "leading_detached_comments", "number": 6, "type": 9, "label": 3 }] }], "extensionRange": [{ "start": 536e6, "end": 536000001 }] }, { "name": "GeneratedCodeInfo", "field": [{ "name": "annotation", "number": 1, "type": 11, "label": 3, "typeName": ".google.protobuf.GeneratedCodeInfo.Annotation" }], "nestedType": [{ "name": "Annotation", "field": [{ "name": "path", "number": 1, "type": 5, "label": 3, "options": { "packed": true } }, { "name": "source_file", "number": 2, "type": 9, "label": 1 }, { "name": "begin", "number": 3, "type": 5, "label": 1 }, { "name": "end", "number": 4, "type": 5, "label": 1 }, { "name": "semantic", "number": 5, "type": 14, "label": 1, "typeName": ".google.protobuf.GeneratedCodeInfo.Annotation.Semantic" }], "enumType": [{ "name": "Semantic", "value": [{ "name": "NONE", "number": 0 }, { "name": "SET", "number": 1 }, { "name": "ALIAS", "number": 2 }] }] }] }], "enumType": [{ "name": "Edition", "value": [{ "name": "EDITION_UNKNOWN", "number": 0 }, { "name": "EDITION_LEGACY", "number": 900 }, { "name": "EDITION_PROTO2", "number": 998 }, { "name": "EDITION_PROTO3", "number": 999 }, { "name": "EDITION_2023", "number": 1e3 }, { "name": "EDITION_2024", "number": 1001 }, { "name": "EDITION_UNSTABLE", "number": 9999 }, { "name": "EDITION_1_TEST_ONLY", "number": 1 }, { "name": "EDITION_2_TEST_ONLY", "number": 2 }, { "name": "EDITION_99997_TEST_ONLY", "number": 99997 }, { "name": "EDITION_99998_TEST_ONLY", "number": 99998 }, { "name": "EDITION_99999_TEST_ONLY", "number": 99999 }, { "name": "EDITION_MAX", "number": 2147483647 }] }, { "name": "SymbolVisibility", "value": [{ "name": "VISIBILITY_UNSET", "number": 0 }, { "name": "VISIBILITY_LOCAL", "number": 1 }, { "name": "VISIBILITY_EXPORT", "number": 2 }] }] });
var FileDescriptorProtoSchema = /* @__PURE__ */ messageDesc(file_google_protobuf_descriptor, 1);
var ExtensionRangeOptions_VerificationState;
(function(ExtensionRangeOptions_VerificationState2) {
  ExtensionRangeOptions_VerificationState2[ExtensionRangeOptions_VerificationState2["DECLARATION"] = 0] = "DECLARATION";
  ExtensionRangeOptions_VerificationState2[ExtensionRangeOptions_VerificationState2["UNVERIFIED"] = 1] = "UNVERIFIED";
})(ExtensionRangeOptions_VerificationState || (ExtensionRangeOptions_VerificationState = {}));
var FieldDescriptorProto_Type;
(function(FieldDescriptorProto_Type2) {
  FieldDescriptorProto_Type2[FieldDescriptorProto_Type2["DOUBLE"] = 1] = "DOUBLE";
  FieldDescriptorProto_Type2[FieldDescriptorProto_Type2["FLOAT"] = 2] = "FLOAT";
  FieldDescriptorProto_Type2[FieldDescriptorProto_Type2["INT64"] = 3] = "INT64";
  FieldDescriptorProto_Type2[FieldDescriptorProto_Type2["UINT64"] = 4] = "UINT64";
  FieldDescriptorProto_Type2[FieldDescriptorProto_Type2["INT32"] = 5] = "INT32";
  FieldDescriptorProto_Type2[FieldDescriptorProto_Type2["FIXED64"] = 6] = "FIXED64";
  FieldDescriptorProto_Type2[FieldDescriptorProto_Type2["FIXED32"] = 7] = "FIXED32";
  FieldDescriptorProto_Type2[FieldDescriptorProto_Type2["BOOL"] = 8] = "BOOL";
  FieldDescriptorProto_Type2[FieldDescriptorProto_Type2["STRING"] = 9] = "STRING";
  FieldDescriptorProto_Type2[FieldDescriptorProto_Type2["GROUP"] = 10] = "GROUP";
  FieldDescriptorProto_Type2[FieldDescriptorProto_Type2["MESSAGE"] = 11] = "MESSAGE";
  FieldDescriptorProto_Type2[FieldDescriptorProto_Type2["BYTES"] = 12] = "BYTES";
  FieldDescriptorProto_Type2[FieldDescriptorProto_Type2["UINT32"] = 13] = "UINT32";
  FieldDescriptorProto_Type2[FieldDescriptorProto_Type2["ENUM"] = 14] = "ENUM";
  FieldDescriptorProto_Type2[FieldDescriptorProto_Type2["SFIXED32"] = 15] = "SFIXED32";
  FieldDescriptorProto_Type2[FieldDescriptorProto_Type2["SFIXED64"] = 16] = "SFIXED64";
  FieldDescriptorProto_Type2[FieldDescriptorProto_Type2["SINT32"] = 17] = "SINT32";
  FieldDescriptorProto_Type2[FieldDescriptorProto_Type2["SINT64"] = 18] = "SINT64";
})(FieldDescriptorProto_Type || (FieldDescriptorProto_Type = {}));
var FieldDescriptorProto_Label;
(function(FieldDescriptorProto_Label2) {
  FieldDescriptorProto_Label2[FieldDescriptorProto_Label2["OPTIONAL"] = 1] = "OPTIONAL";
  FieldDescriptorProto_Label2[FieldDescriptorProto_Label2["REPEATED"] = 3] = "REPEATED";
  FieldDescriptorProto_Label2[FieldDescriptorProto_Label2["REQUIRED"] = 2] = "REQUIRED";
})(FieldDescriptorProto_Label || (FieldDescriptorProto_Label = {}));
var FileOptions_OptimizeMode;
(function(FileOptions_OptimizeMode2) {
  FileOptions_OptimizeMode2[FileOptions_OptimizeMode2["SPEED"] = 1] = "SPEED";
  FileOptions_OptimizeMode2[FileOptions_OptimizeMode2["CODE_SIZE"] = 2] = "CODE_SIZE";
  FileOptions_OptimizeMode2[FileOptions_OptimizeMode2["LITE_RUNTIME"] = 3] = "LITE_RUNTIME";
})(FileOptions_OptimizeMode || (FileOptions_OptimizeMode = {}));
var FieldOptions_CType;
(function(FieldOptions_CType2) {
  FieldOptions_CType2[FieldOptions_CType2["STRING"] = 0] = "STRING";
  FieldOptions_CType2[FieldOptions_CType2["CORD"] = 1] = "CORD";
  FieldOptions_CType2[FieldOptions_CType2["STRING_PIECE"] = 2] = "STRING_PIECE";
})(FieldOptions_CType || (FieldOptions_CType = {}));
var FieldOptions_JSType;
(function(FieldOptions_JSType2) {
  FieldOptions_JSType2[FieldOptions_JSType2["JS_NORMAL"] = 0] = "JS_NORMAL";
  FieldOptions_JSType2[FieldOptions_JSType2["JS_STRING"] = 1] = "JS_STRING";
  FieldOptions_JSType2[FieldOptions_JSType2["JS_NUMBER"] = 2] = "JS_NUMBER";
})(FieldOptions_JSType || (FieldOptions_JSType = {}));
var FieldOptions_OptionRetention;
(function(FieldOptions_OptionRetention2) {
  FieldOptions_OptionRetention2[FieldOptions_OptionRetention2["RETENTION_UNKNOWN"] = 0] = "RETENTION_UNKNOWN";
  FieldOptions_OptionRetention2[FieldOptions_OptionRetention2["RETENTION_RUNTIME"] = 1] = "RETENTION_RUNTIME";
  FieldOptions_OptionRetention2[FieldOptions_OptionRetention2["RETENTION_SOURCE"] = 2] = "RETENTION_SOURCE";
})(FieldOptions_OptionRetention || (FieldOptions_OptionRetention = {}));
var FieldOptions_OptionTargetType;
(function(FieldOptions_OptionTargetType2) {
  FieldOptions_OptionTargetType2[FieldOptions_OptionTargetType2["TARGET_TYPE_UNKNOWN"] = 0] = "TARGET_TYPE_UNKNOWN";
  FieldOptions_OptionTargetType2[FieldOptions_OptionTargetType2["TARGET_TYPE_FILE"] = 1] = "TARGET_TYPE_FILE";
  FieldOptions_OptionTargetType2[FieldOptions_OptionTargetType2["TARGET_TYPE_EXTENSION_RANGE"] = 2] = "TARGET_TYPE_EXTENSION_RANGE";
  FieldOptions_OptionTargetType2[FieldOptions_OptionTargetType2["TARGET_TYPE_MESSAGE"] = 3] = "TARGET_TYPE_MESSAGE";
  FieldOptions_OptionTargetType2[FieldOptions_OptionTargetType2["TARGET_TYPE_FIELD"] = 4] = "TARGET_TYPE_FIELD";
  FieldOptions_OptionTargetType2[FieldOptions_OptionTargetType2["TARGET_TYPE_ONEOF"] = 5] = "TARGET_TYPE_ONEOF";
  FieldOptions_OptionTargetType2[FieldOptions_OptionTargetType2["TARGET_TYPE_ENUM"] = 6] = "TARGET_TYPE_ENUM";
  FieldOptions_OptionTargetType2[FieldOptions_OptionTargetType2["TARGET_TYPE_ENUM_ENTRY"] = 7] = "TARGET_TYPE_ENUM_ENTRY";
  FieldOptions_OptionTargetType2[FieldOptions_OptionTargetType2["TARGET_TYPE_SERVICE"] = 8] = "TARGET_TYPE_SERVICE";
  FieldOptions_OptionTargetType2[FieldOptions_OptionTargetType2["TARGET_TYPE_METHOD"] = 9] = "TARGET_TYPE_METHOD";
})(FieldOptions_OptionTargetType || (FieldOptions_OptionTargetType = {}));
var MethodOptions_IdempotencyLevel;
(function(MethodOptions_IdempotencyLevel2) {
  MethodOptions_IdempotencyLevel2[MethodOptions_IdempotencyLevel2["IDEMPOTENCY_UNKNOWN"] = 0] = "IDEMPOTENCY_UNKNOWN";
  MethodOptions_IdempotencyLevel2[MethodOptions_IdempotencyLevel2["NO_SIDE_EFFECTS"] = 1] = "NO_SIDE_EFFECTS";
  MethodOptions_IdempotencyLevel2[MethodOptions_IdempotencyLevel2["IDEMPOTENT"] = 2] = "IDEMPOTENT";
})(MethodOptions_IdempotencyLevel || (MethodOptions_IdempotencyLevel = {}));
var FeatureSet_VisibilityFeature_DefaultSymbolVisibility;
(function(FeatureSet_VisibilityFeature_DefaultSymbolVisibility2) {
  FeatureSet_VisibilityFeature_DefaultSymbolVisibility2[FeatureSet_VisibilityFeature_DefaultSymbolVisibility2["DEFAULT_SYMBOL_VISIBILITY_UNKNOWN"] = 0] = "DEFAULT_SYMBOL_VISIBILITY_UNKNOWN";
  FeatureSet_VisibilityFeature_DefaultSymbolVisibility2[FeatureSet_VisibilityFeature_DefaultSymbolVisibility2["EXPORT_ALL"] = 1] = "EXPORT_ALL";
  FeatureSet_VisibilityFeature_DefaultSymbolVisibility2[FeatureSet_VisibilityFeature_DefaultSymbolVisibility2["EXPORT_TOP_LEVEL"] = 2] = "EXPORT_TOP_LEVEL";
  FeatureSet_VisibilityFeature_DefaultSymbolVisibility2[FeatureSet_VisibilityFeature_DefaultSymbolVisibility2["LOCAL_ALL"] = 3] = "LOCAL_ALL";
  FeatureSet_VisibilityFeature_DefaultSymbolVisibility2[FeatureSet_VisibilityFeature_DefaultSymbolVisibility2["STRICT"] = 4] = "STRICT";
})(FeatureSet_VisibilityFeature_DefaultSymbolVisibility || (FeatureSet_VisibilityFeature_DefaultSymbolVisibility = {}));
var FeatureSet_FieldPresence;
(function(FeatureSet_FieldPresence2) {
  FeatureSet_FieldPresence2[FeatureSet_FieldPresence2["FIELD_PRESENCE_UNKNOWN"] = 0] = "FIELD_PRESENCE_UNKNOWN";
  FeatureSet_FieldPresence2[FeatureSet_FieldPresence2["EXPLICIT"] = 1] = "EXPLICIT";
  FeatureSet_FieldPresence2[FeatureSet_FieldPresence2["IMPLICIT"] = 2] = "IMPLICIT";
  FeatureSet_FieldPresence2[FeatureSet_FieldPresence2["LEGACY_REQUIRED"] = 3] = "LEGACY_REQUIRED";
})(FeatureSet_FieldPresence || (FeatureSet_FieldPresence = {}));
var FeatureSet_EnumType;
(function(FeatureSet_EnumType2) {
  FeatureSet_EnumType2[FeatureSet_EnumType2["ENUM_TYPE_UNKNOWN"] = 0] = "ENUM_TYPE_UNKNOWN";
  FeatureSet_EnumType2[FeatureSet_EnumType2["OPEN"] = 1] = "OPEN";
  FeatureSet_EnumType2[FeatureSet_EnumType2["CLOSED"] = 2] = "CLOSED";
})(FeatureSet_EnumType || (FeatureSet_EnumType = {}));
var FeatureSet_RepeatedFieldEncoding;
(function(FeatureSet_RepeatedFieldEncoding2) {
  FeatureSet_RepeatedFieldEncoding2[FeatureSet_RepeatedFieldEncoding2["REPEATED_FIELD_ENCODING_UNKNOWN"] = 0] = "REPEATED_FIELD_ENCODING_UNKNOWN";
  FeatureSet_RepeatedFieldEncoding2[FeatureSet_RepeatedFieldEncoding2["PACKED"] = 1] = "PACKED";
  FeatureSet_RepeatedFieldEncoding2[FeatureSet_RepeatedFieldEncoding2["EXPANDED"] = 2] = "EXPANDED";
})(FeatureSet_RepeatedFieldEncoding || (FeatureSet_RepeatedFieldEncoding = {}));
var FeatureSet_Utf8Validation;
(function(FeatureSet_Utf8Validation2) {
  FeatureSet_Utf8Validation2[FeatureSet_Utf8Validation2["UTF8_VALIDATION_UNKNOWN"] = 0] = "UTF8_VALIDATION_UNKNOWN";
  FeatureSet_Utf8Validation2[FeatureSet_Utf8Validation2["VERIFY"] = 2] = "VERIFY";
  FeatureSet_Utf8Validation2[FeatureSet_Utf8Validation2["NONE"] = 3] = "NONE";
})(FeatureSet_Utf8Validation || (FeatureSet_Utf8Validation = {}));
var FeatureSet_MessageEncoding;
(function(FeatureSet_MessageEncoding2) {
  FeatureSet_MessageEncoding2[FeatureSet_MessageEncoding2["MESSAGE_ENCODING_UNKNOWN"] = 0] = "MESSAGE_ENCODING_UNKNOWN";
  FeatureSet_MessageEncoding2[FeatureSet_MessageEncoding2["LENGTH_PREFIXED"] = 1] = "LENGTH_PREFIXED";
  FeatureSet_MessageEncoding2[FeatureSet_MessageEncoding2["DELIMITED"] = 2] = "DELIMITED";
})(FeatureSet_MessageEncoding || (FeatureSet_MessageEncoding = {}));
var FeatureSet_JsonFormat;
(function(FeatureSet_JsonFormat2) {
  FeatureSet_JsonFormat2[FeatureSet_JsonFormat2["JSON_FORMAT_UNKNOWN"] = 0] = "JSON_FORMAT_UNKNOWN";
  FeatureSet_JsonFormat2[FeatureSet_JsonFormat2["ALLOW"] = 1] = "ALLOW";
  FeatureSet_JsonFormat2[FeatureSet_JsonFormat2["LEGACY_BEST_EFFORT"] = 2] = "LEGACY_BEST_EFFORT";
})(FeatureSet_JsonFormat || (FeatureSet_JsonFormat = {}));
var FeatureSet_EnforceNamingStyle;
(function(FeatureSet_EnforceNamingStyle2) {
  FeatureSet_EnforceNamingStyle2[FeatureSet_EnforceNamingStyle2["ENFORCE_NAMING_STYLE_UNKNOWN"] = 0] = "ENFORCE_NAMING_STYLE_UNKNOWN";
  FeatureSet_EnforceNamingStyle2[FeatureSet_EnforceNamingStyle2["STYLE2024"] = 1] = "STYLE2024";
  FeatureSet_EnforceNamingStyle2[FeatureSet_EnforceNamingStyle2["STYLE_LEGACY"] = 2] = "STYLE_LEGACY";
})(FeatureSet_EnforceNamingStyle || (FeatureSet_EnforceNamingStyle = {}));
var GeneratedCodeInfo_Annotation_Semantic;
(function(GeneratedCodeInfo_Annotation_Semantic2) {
  GeneratedCodeInfo_Annotation_Semantic2[GeneratedCodeInfo_Annotation_Semantic2["NONE"] = 0] = "NONE";
  GeneratedCodeInfo_Annotation_Semantic2[GeneratedCodeInfo_Annotation_Semantic2["SET"] = 1] = "SET";
  GeneratedCodeInfo_Annotation_Semantic2[GeneratedCodeInfo_Annotation_Semantic2["ALIAS"] = 2] = "ALIAS";
})(GeneratedCodeInfo_Annotation_Semantic || (GeneratedCodeInfo_Annotation_Semantic = {}));
var Edition;
(function(Edition2) {
  Edition2[Edition2["EDITION_UNKNOWN"] = 0] = "EDITION_UNKNOWN";
  Edition2[Edition2["EDITION_LEGACY"] = 900] = "EDITION_LEGACY";
  Edition2[Edition2["EDITION_PROTO2"] = 998] = "EDITION_PROTO2";
  Edition2[Edition2["EDITION_PROTO3"] = 999] = "EDITION_PROTO3";
  Edition2[Edition2["EDITION_2023"] = 1e3] = "EDITION_2023";
  Edition2[Edition2["EDITION_2024"] = 1001] = "EDITION_2024";
  Edition2[Edition2["EDITION_UNSTABLE"] = 9999] = "EDITION_UNSTABLE";
  Edition2[Edition2["EDITION_1_TEST_ONLY"] = 1] = "EDITION_1_TEST_ONLY";
  Edition2[Edition2["EDITION_2_TEST_ONLY"] = 2] = "EDITION_2_TEST_ONLY";
  Edition2[Edition2["EDITION_99997_TEST_ONLY"] = 99997] = "EDITION_99997_TEST_ONLY";
  Edition2[Edition2["EDITION_99998_TEST_ONLY"] = 99998] = "EDITION_99998_TEST_ONLY";
  Edition2[Edition2["EDITION_99999_TEST_ONLY"] = 99999] = "EDITION_99999_TEST_ONLY";
  Edition2[Edition2["EDITION_MAX"] = 2147483647] = "EDITION_MAX";
})(Edition || (Edition = {}));
var SymbolVisibility;
(function(SymbolVisibility2) {
  SymbolVisibility2[SymbolVisibility2["VISIBILITY_UNSET"] = 0] = "VISIBILITY_UNSET";
  SymbolVisibility2[SymbolVisibility2["VISIBILITY_LOCAL"] = 1] = "VISIBILITY_LOCAL";
  SymbolVisibility2[SymbolVisibility2["VISIBILITY_EXPORT"] = 2] = "VISIBILITY_EXPORT";
})(SymbolVisibility || (SymbolVisibility = {}));

// node_modules/.pnpm/@bufbuild+protobuf@2.12.1/node_modules/@bufbuild/protobuf/dist/esm/from-binary.js
function makeReadContext(options) {
  return Object.assign(Object.assign({ readUnknownFields: true, recursionLimit: 100 }, options), { depth: 0 });
}
function fromBinary(schema, bytes, options) {
  const msg = reflect(schema, void 0, false);
  readMessage(msg, new BinaryReader(bytes), makeReadContext(options), false, bytes.byteLength);
  return msg.message;
}
function readMessage(message, reader, ctx, delimited, lengthOrDelimitedFieldNo) {
  var _a2;
  if (++ctx.depth > ctx.recursionLimit) {
    throw new Error(`cannot decode ${message.desc} from binary: maximum recursion depth of ${ctx.recursionLimit} reached`);
  }
  const end = delimited ? reader.len : reader.pos + lengthOrDelimitedFieldNo;
  let fieldNo;
  let wireType;
  const unknownFields = (_a2 = message.getUnknown()) !== null && _a2 !== void 0 ? _a2 : [];
  while (reader.pos < end) {
    [fieldNo, wireType] = reader.tag();
    if (delimited && wireType == WireType.EndGroup) {
      break;
    }
    const field = message.findNumber(fieldNo);
    if (!field) {
      const recursionLimit = ctx.recursionLimit - ctx.depth;
      const data = reader.skip(wireType, fieldNo, recursionLimit);
      if (ctx.readUnknownFields) {
        unknownFields.push({ no: fieldNo, wireType, data });
      }
      continue;
    }
    readField(message, reader, field, wireType, ctx);
  }
  if (delimited) {
    if (wireType != WireType.EndGroup || fieldNo !== lengthOrDelimitedFieldNo) {
      throw new Error("invalid end group tag");
    }
  }
  if (unknownFields.length > 0) {
    message.setUnknown(unknownFields);
  }
  ctx.depth--;
}
function readField(message, reader, field, wireType, ctx) {
  var _a2;
  switch (field.fieldKind) {
    case "scalar":
      message.set(field, readScalar(reader, field.scalar, field.utf8Validation));
      break;
    case "enum":
      const val = readScalar(reader, ScalarType.INT32);
      if (field.enum.open) {
        message.set(field, val);
      } else {
        const ok = field.enum.values.some((v) => v.number === val);
        if (ok) {
          message.set(field, val);
        } else if (ctx.readUnknownFields) {
          const bytes = [];
          varint32write(val, bytes);
          const unknownFields = (_a2 = message.getUnknown()) !== null && _a2 !== void 0 ? _a2 : [];
          unknownFields.push({
            no: field.number,
            wireType,
            data: new Uint8Array(bytes)
          });
          message.setUnknown(unknownFields);
        }
      }
      break;
    case "message":
      message.set(field, readMessageField(reader, ctx, field, message.get(field)));
      break;
    case "list":
      readListField(reader, wireType, message.get(field), ctx);
      break;
    case "map":
      readMapEntry(reader, message.get(field), ctx);
      break;
  }
}
function readMapEntry(reader, map, ctx) {
  const field = map.field();
  let key;
  let val;
  const len = reader.uint32();
  const end = reader.pos + len;
  while (reader.pos < end) {
    const [fieldNo] = reader.tag();
    switch (fieldNo) {
      case 1:
        key = readScalar(reader, field.mapKey, field.utf8Validation);
        break;
      case 2:
        switch (field.mapKind) {
          case "scalar":
            val = readScalar(reader, field.scalar, field.utf8Validation);
            break;
          case "enum":
            val = reader.int32();
            break;
          case "message":
            val = readMessageField(reader, ctx, field);
            break;
        }
        break;
    }
  }
  if (key === void 0) {
    key = scalarZeroValue(field.mapKey, false);
  }
  if (val === void 0) {
    switch (field.mapKind) {
      case "scalar":
        val = scalarZeroValue(field.scalar, false);
        break;
      case "enum":
        val = field.enum.values[0].number;
        break;
      case "message":
        val = reflect(field.message, void 0, false);
        break;
    }
  }
  map.set(key, val);
}
function readListField(reader, wireType, list, ctx) {
  var _a2;
  const field = list.field();
  if (field.listKind === "message") {
    list.add(readMessageField(reader, ctx, field));
    return;
  }
  const scalarType = (_a2 = field.scalar) !== null && _a2 !== void 0 ? _a2 : ScalarType.INT32;
  const packed = wireType == WireType.LengthDelimited && scalarType != ScalarType.STRING && scalarType != ScalarType.BYTES;
  if (!packed) {
    list.add(readScalar(reader, scalarType, field.utf8Validation));
    return;
  }
  const e = reader.uint32() + reader.pos;
  while (reader.pos < e) {
    list.add(readScalar(reader, scalarType, field.utf8Validation));
  }
}
function readMessageField(reader, ctx, field, mergeMessage) {
  const delimited = field.delimitedEncoding;
  const message = mergeMessage !== null && mergeMessage !== void 0 ? mergeMessage : reflect(field.message, void 0, false);
  readMessage(message, reader, ctx, delimited, delimited ? field.number : reader.uint32());
  return message;
}
function readScalar(reader, type, validateUtf8 = false) {
  switch (type) {
    case ScalarType.STRING:
      return reader.string(validateUtf8);
    case ScalarType.BOOL:
      return reader.bool();
    case ScalarType.DOUBLE:
      return reader.double();
    case ScalarType.FLOAT:
      return reader.float();
    case ScalarType.INT32:
      return reader.int32();
    case ScalarType.INT64:
      return reader.int64();
    case ScalarType.UINT64:
      return reader.uint64();
    case ScalarType.FIXED64:
      return reader.fixed64();
    case ScalarType.BYTES:
      return reader.bytes();
    case ScalarType.FIXED32:
      return reader.fixed32();
    case ScalarType.SFIXED32:
      return reader.sfixed32();
    case ScalarType.SFIXED64:
      return reader.sfixed64();
    case ScalarType.SINT64:
      return reader.sint64();
    case ScalarType.UINT32:
      return reader.uint32();
    case ScalarType.SINT32:
      return reader.sint32();
  }
}

// node_modules/.pnpm/@bufbuild+protobuf@2.12.1/node_modules/@bufbuild/protobuf/dist/esm/codegenv2/file.js
function fileDesc(b64, imports) {
  var _a2;
  const root2 = fromBinary(FileDescriptorProtoSchema, base64Decode(b64));
  root2.messageType.forEach(restoreJsonNames);
  root2.dependency = (_a2 = imports === null || imports === void 0 ? void 0 : imports.map((f) => f.proto.name)) !== null && _a2 !== void 0 ? _a2 : [];
  const reg = createFileRegistry(root2, (protoFileName) => imports === null || imports === void 0 ? void 0 : imports.find((f) => f.proto.name === protoFileName));
  return reg.getFile(root2.name);
}

// node_modules/.pnpm/@bufbuild+protobuf@2.12.1/node_modules/@bufbuild/protobuf/dist/esm/wkt/gen/google/protobuf/timestamp_pb.js
var file_google_protobuf_timestamp = /* @__PURE__ */ fileDesc("Ch9nb29nbGUvcHJvdG9idWYvdGltZXN0YW1wLnByb3RvEg9nb29nbGUucHJvdG9idWYiKwoJVGltZXN0YW1wEg8KB3NlY29uZHMYASABKAMSDQoFbmFub3MYAiABKAVChQEKE2NvbS5nb29nbGUucHJvdG9idWZCDlRpbWVzdGFtcFByb3RvUAFaMmdvb2dsZS5nb2xhbmcub3JnL3Byb3RvYnVmL3R5cGVzL2tub3duL3RpbWVzdGFtcHBi+AEBogIDR1BCqgIeR29vZ2xlLlByb3RvYnVmLldlbGxLbm93blR5cGVzYgZwcm90bzM");

// node_modules/.pnpm/@bufbuild+protobuf@2.12.1/node_modules/@bufbuild/protobuf/dist/esm/wkt/gen/google/protobuf/duration_pb.js
var file_google_protobuf_duration = /* @__PURE__ */ fileDesc("Ch5nb29nbGUvcHJvdG9idWYvZHVyYXRpb24ucHJvdG8SD2dvb2dsZS5wcm90b2J1ZiIqCghEdXJhdGlvbhIPCgdzZWNvbmRzGAEgASgDEg0KBW5hbm9zGAIgASgFQoMBChNjb20uZ29vZ2xlLnByb3RvYnVmQg1EdXJhdGlvblByb3RvUAFaMWdvb2dsZS5nb2xhbmcub3JnL3Byb3RvYnVmL3R5cGVzL2tub3duL2R1cmF0aW9ucGL4AQGiAgNHUEKqAh5Hb29nbGUuUHJvdG9idWYuV2VsbEtub3duVHlwZXNiBnByb3RvMw");

// node_modules/.pnpm/@bufbuild+protobuf@2.12.1/node_modules/@bufbuild/protobuf/dist/esm/wkt/gen/google/protobuf/any_pb.js
var file_google_protobuf_any = /* @__PURE__ */ fileDesc("Chlnb29nbGUvcHJvdG9idWYvYW55LnByb3RvEg9nb29nbGUucHJvdG9idWYiJgoDQW55EhAKCHR5cGVfdXJsGAEgASgJEg0KBXZhbHVlGAIgASgMQnYKE2NvbS5nb29nbGUucHJvdG9idWZCCEFueVByb3RvUAFaLGdvb2dsZS5nb2xhbmcub3JnL3Byb3RvYnVmL3R5cGVzL2tub3duL2FueXBiogIDR1BCqgIeR29vZ2xlLlByb3RvYnVmLldlbGxLbm93blR5cGVzYgZwcm90bzM");
var AnySchema = /* @__PURE__ */ messageDesc(file_google_protobuf_any, 0);

// node_modules/.pnpm/@bufbuild+protobuf@2.12.1/node_modules/@bufbuild/protobuf/dist/esm/to-binary.js
var LEGACY_REQUIRED2 = 3;
var writeDefaults = {
  writeUnknownFields: true
};
function makeWriteOptions(options) {
  return options ? Object.assign(Object.assign({}, writeDefaults), options) : writeDefaults;
}
function toBinary(schema, message, options) {
  return writeFields(new BinaryWriter(), makeWriteOptions(options), reflect(schema, message)).finish();
}
function writeFields(writer, opts, msg) {
  var _a2;
  for (const f of msg.sortedFields) {
    if (!msg.isSet(f)) {
      if (f.presence == LEGACY_REQUIRED2) {
        throw new Error(`cannot encode ${f} to binary: required field not set`);
      }
      continue;
    }
    writeField(writer, opts, msg, f);
  }
  if (opts.writeUnknownFields) {
    for (const { no, wireType, data } of (_a2 = msg.getUnknown()) !== null && _a2 !== void 0 ? _a2 : []) {
      writer.tag(no, wireType).raw(data);
    }
  }
  return writer;
}
function writeField(writer, opts, msg, field) {
  var _a2;
  switch (field.fieldKind) {
    case "scalar":
    case "enum":
      writeScalar(writer, msg.desc.typeName, field.name, (_a2 = field.scalar) !== null && _a2 !== void 0 ? _a2 : ScalarType.INT32, field.number, msg.get(field));
      break;
    case "list":
      writeListField(writer, opts, field, msg.get(field));
      break;
    case "message":
      writeMessageField(writer, opts, field, msg.get(field));
      break;
    case "map":
      for (const [key, val] of msg.get(field)) {
        writeMapEntry(writer, opts, field, key, val);
      }
      break;
  }
}
function writeScalar(writer, msgName, fieldName, scalarType, fieldNo, value) {
  writeScalarValue(writer.tag(fieldNo, writeTypeOfScalar(scalarType)), msgName, fieldName, scalarType, value);
}
function writeMessageField(writer, opts, field, message) {
  if (field.delimitedEncoding) {
    writeFields(writer.tag(field.number, WireType.StartGroup), opts, message).tag(field.number, WireType.EndGroup);
  } else {
    writeFields(writer.tag(field.number, WireType.LengthDelimited).fork(), opts, message).join();
  }
}
function writeListField(writer, opts, field, list) {
  var _a2;
  if (field.listKind == "message") {
    for (const item of list) {
      writeMessageField(writer, opts, field, item);
    }
    return;
  }
  const scalarType = (_a2 = field.scalar) !== null && _a2 !== void 0 ? _a2 : ScalarType.INT32;
  if (field.packed) {
    if (!list.size) {
      return;
    }
    writer.tag(field.number, WireType.LengthDelimited).fork();
    for (const item of list) {
      writeScalarValue(writer, field.parent.typeName, field.name, scalarType, item);
    }
    writer.join();
    return;
  }
  for (const item of list) {
    writeScalar(writer, field.parent.typeName, field.name, scalarType, field.number, item);
  }
}
function writeMapEntry(writer, opts, field, key, value) {
  var _a2;
  writer.tag(field.number, WireType.LengthDelimited).fork();
  writeScalar(writer, field.parent.typeName, field.name, field.mapKey, 1, key);
  switch (field.mapKind) {
    case "scalar":
    case "enum":
      writeScalar(writer, field.parent.typeName, field.name, (_a2 = field.scalar) !== null && _a2 !== void 0 ? _a2 : ScalarType.INT32, 2, value);
      break;
    case "message":
      writeFields(writer.tag(2, WireType.LengthDelimited).fork(), opts, value).join();
      break;
  }
  writer.join();
}
function writeScalarValue(writer, msgName, fieldName, type, value) {
  try {
    switch (type) {
      case ScalarType.STRING:
        writer.string(value);
        break;
      case ScalarType.BOOL:
        writer.bool(value);
        break;
      case ScalarType.DOUBLE:
        writer.double(value);
        break;
      case ScalarType.FLOAT:
        writer.float(value);
        break;
      case ScalarType.INT32:
        writer.int32(value);
        break;
      case ScalarType.INT64:
        writer.int64(value);
        break;
      case ScalarType.UINT64:
        writer.uint64(value);
        break;
      case ScalarType.FIXED64:
        writer.fixed64(value);
        break;
      case ScalarType.BYTES:
        writer.bytes(value);
        break;
      case ScalarType.FIXED32:
        writer.fixed32(value);
        break;
      case ScalarType.SFIXED32:
        writer.sfixed32(value);
        break;
      case ScalarType.SFIXED64:
        writer.sfixed64(value);
        break;
      case ScalarType.SINT64:
        writer.sint64(value);
        break;
      case ScalarType.UINT32:
        writer.uint32(value);
        break;
      case ScalarType.SINT32:
        writer.sint32(value);
        break;
    }
  } catch (e) {
    if (e instanceof Error) {
      throw new Error(`cannot encode field ${msgName}.${fieldName} to binary: ${e.message}`);
    }
    throw e;
  }
}
function writeTypeOfScalar(type) {
  switch (type) {
    case ScalarType.BYTES:
    case ScalarType.STRING:
      return WireType.LengthDelimited;
    case ScalarType.DOUBLE:
    case ScalarType.FIXED64:
    case ScalarType.SFIXED64:
      return WireType.Bit64;
    case ScalarType.FIXED32:
    case ScalarType.SFIXED32:
    case ScalarType.FLOAT:
      return WireType.Bit32;
    default:
      return WireType.Varint;
  }
}

// node_modules/.pnpm/@bufbuild+protobuf@2.12.1/node_modules/@bufbuild/protobuf/dist/esm/wkt/gen/google/protobuf/field_mask_pb.js
var file_google_protobuf_field_mask = /* @__PURE__ */ fileDesc("CiBnb29nbGUvcHJvdG9idWYvZmllbGRfbWFzay5wcm90bxIPZ29vZ2xlLnByb3RvYnVmIhoKCUZpZWxkTWFzaxINCgVwYXRocxgBIAMoCUKFAQoTY29tLmdvb2dsZS5wcm90b2J1ZkIORmllbGRNYXNrUHJvdG9QAVoyZ29vZ2xlLmdvbGFuZy5vcmcvcHJvdG9idWYvdHlwZXMva25vd24vZmllbGRtYXNrcGL4AQGiAgNHUEKqAh5Hb29nbGUuUHJvdG9idWYuV2VsbEtub3duVHlwZXNiBnByb3RvMw");

// node_modules/.pnpm/@buf+bufbuild_protovalidate.bufbuild_es@2.12.1-20260709200747-435963d16310.1_@bufbuild+protobuf@2.12.1/node_modules/@buf/bufbuild_protovalidate.bufbuild_es/buf/validate/validate_pb.js
var file_buf_validate_validate = /* @__PURE__ */ fileDesc("ChtidWYvdmFsaWRhdGUvdmFsaWRhdGUucHJvdG8SDGJ1Zi52YWxpZGF0ZSI3CgRSdWxlEgoKAmlkGAEgASgJEg8KB21lc3NhZ2UYAiABKAkSEgoKZXhwcmVzc2lvbhgDIAEoCSKGAQoMTWVzc2FnZVJ1bGVzEhYKDmNlbF9leHByZXNzaW9uGAUgAygJEh8KA2NlbBgDIAMoCzISLmJ1Zi52YWxpZGF0ZS5SdWxlEi0KBW9uZW9mGAQgAygLMh4uYnVmLnZhbGlkYXRlLk1lc3NhZ2VPbmVvZlJ1bGVKBAgBEAJSCGRpc2FibGVkIjQKEE1lc3NhZ2VPbmVvZlJ1bGUSDgoGZmllbGRzGAEgAygJEhAKCHJlcXVpcmVkGAIgASgIIh4KCk9uZW9mUnVsZXMSEAoIcmVxdWlyZWQYASABKAgiiwkKCkZpZWxkUnVsZXMSFgoOY2VsX2V4cHJlc3Npb24YHSADKAkSHwoDY2VsGBcgAygLMhIuYnVmLnZhbGlkYXRlLlJ1bGUSEAoIcmVxdWlyZWQYGSABKAgSJAoGaWdub3JlGBsgASgOMhQuYnVmLnZhbGlkYXRlLklnbm9yZRIpCgVmbG9hdBgBIAEoCzIYLmJ1Zi52YWxpZGF0ZS5GbG9hdFJ1bGVzSAASKwoGZG91YmxlGAIgASgLMhkuYnVmLnZhbGlkYXRlLkRvdWJsZVJ1bGVzSAASKQoFaW50MzIYAyABKAsyGC5idWYudmFsaWRhdGUuSW50MzJSdWxlc0gAEikKBWludDY0GAQgASgLMhguYnVmLnZhbGlkYXRlLkludDY0UnVsZXNIABIrCgZ1aW50MzIYBSABKAsyGS5idWYudmFsaWRhdGUuVUludDMyUnVsZXNIABIrCgZ1aW50NjQYBiABKAsyGS5idWYudmFsaWRhdGUuVUludDY0UnVsZXNIABIrCgZzaW50MzIYByABKAsyGS5idWYudmFsaWRhdGUuU0ludDMyUnVsZXNIABIrCgZzaW50NjQYCCABKAsyGS5idWYudmFsaWRhdGUuU0ludDY0UnVsZXNIABItCgdmaXhlZDMyGAkgASgLMhouYnVmLnZhbGlkYXRlLkZpeGVkMzJSdWxlc0gAEi0KB2ZpeGVkNjQYCiABKAsyGi5idWYudmFsaWRhdGUuRml4ZWQ2NFJ1bGVzSAASLwoIc2ZpeGVkMzIYCyABKAsyGy5idWYudmFsaWRhdGUuU0ZpeGVkMzJSdWxlc0gAEi8KCHNmaXhlZDY0GAwgASgLMhsuYnVmLnZhbGlkYXRlLlNGaXhlZDY0UnVsZXNIABInCgRib29sGA0gASgLMhcuYnVmLnZhbGlkYXRlLkJvb2xSdWxlc0gAEisKBnN0cmluZxgOIAEoCzIZLmJ1Zi52YWxpZGF0ZS5TdHJpbmdSdWxlc0gAEikKBWJ5dGVzGA8gASgLMhguYnVmLnZhbGlkYXRlLkJ5dGVzUnVsZXNIABInCgRlbnVtGBAgASgLMhcuYnVmLnZhbGlkYXRlLkVudW1SdWxlc0gAEi8KCHJlcGVhdGVkGBIgASgLMhsuYnVmLnZhbGlkYXRlLlJlcGVhdGVkUnVsZXNIABIlCgNtYXAYEyABKAsyFi5idWYudmFsaWRhdGUuTWFwUnVsZXNIABIlCgNhbnkYFCABKAsyFi5idWYudmFsaWRhdGUuQW55UnVsZXNIABIvCghkdXJhdGlvbhgVIAEoCzIbLmJ1Zi52YWxpZGF0ZS5EdXJhdGlvblJ1bGVzSAASMgoKZmllbGRfbWFzaxgcIAEoCzIcLmJ1Zi52YWxpZGF0ZS5GaWVsZE1hc2tSdWxlc0gAEjEKCXRpbWVzdGFtcBgWIAEoCzIcLmJ1Zi52YWxpZGF0ZS5UaW1lc3RhbXBSdWxlc0gAQgYKBHR5cGVKBAgYEBlKBAgaEBtSB3NraXBwZWRSDGlnbm9yZV9lbXB0eSJVCg9QcmVkZWZpbmVkUnVsZXMSHwoDY2VsGAEgAygLMhIuYnVmLnZhbGlkYXRlLlJ1bGVKBAgYEBlKBAgaEBtSB3NraXBwZWRSDGlnbm9yZV9lbXB0eSL4FgoKRmxvYXRSdWxlcxJ9CgVjb25zdBgBIAEoAkJuwkhrCmkKC2Zsb2F0LmNvbnN0Glp0aGlzICE9IGdldEZpZWxkKHJ1bGVzLCAnY29uc3QnKSA/ICdtdXN0IGVxdWFsICVzJy5mb3JtYXQoW2dldEZpZWxkKHJ1bGVzLCAnY29uc3QnKV0pIDogJycSmQEKAmx0GAIgASgCQooBwkiGAQqDAQoIZmxvYXQubHQadyFoYXMocnVsZXMuZ3RlKSAmJiAhaGFzKHJ1bGVzLmd0KSAmJiAodGhpcy5pc05hbigpIHx8IHRoaXMgPj0gcnVsZXMubHQpPyAnbXVzdCBiZSBsZXNzIHRoYW4gJXMnLmZvcm1hdChbcnVsZXMubHRdKSA6ICcnSAASqQEKA2x0ZRgDIAEoAkKZAcJIlQEKkgEKCWZsb2F0Lmx0ZRqEASFoYXMocnVsZXMuZ3RlKSAmJiAhaGFzKHJ1bGVzLmd0KSAmJiAodGhpcy5pc05hbigpIHx8IHRoaXMgPiBydWxlcy5sdGUpPyAnbXVzdCBiZSBsZXNzIHRoYW4gb3IgZXF1YWwgdG8gJXMnLmZvcm1hdChbcnVsZXMubHRlXSkgOiAnJ0gAEtAHCgJndBgEIAEoAkLBB8JIvQcKhgEKCGZsb2F0Lmd0GnohaGFzKHJ1bGVzLmx0KSAmJiAhaGFzKHJ1bGVzLmx0ZSkgJiYgKHRoaXMuaXNOYW4oKSB8fCB0aGlzIDw9IHJ1bGVzLmd0KT8gJ211c3QgYmUgZ3JlYXRlciB0aGFuICVzJy5mb3JtYXQoW3J1bGVzLmd0XSkgOiAnJwq9AQoLZmxvYXQuZ3RfbHQarQFoYXMocnVsZXMubHQpICYmIHJ1bGVzLmx0ID49IHJ1bGVzLmd0ICYmICh0aGlzLmlzTmFuKCkgfHwgdGhpcyA+PSBydWxlcy5sdCB8fCB0aGlzIDw9IHJ1bGVzLmd0KT8gJ211c3QgYmUgZ3JlYXRlciB0aGFuICVzIGFuZCBsZXNzIHRoYW4gJXMnLmZvcm1hdChbcnVsZXMuZ3QsIHJ1bGVzLmx0XSkgOiAnJwrHAQoVZmxvYXQuZ3RfbHRfZXhjbHVzaXZlGq0BaGFzKHJ1bGVzLmx0KSAmJiBydWxlcy5sdCA8IHJ1bGVzLmd0ICYmICh0aGlzLmlzTmFuKCkgfHwgKHJ1bGVzLmx0IDw9IHRoaXMgJiYgdGhpcyA8PSBydWxlcy5ndCkpPyAnbXVzdCBiZSBncmVhdGVyIHRoYW4gJXMgb3IgbGVzcyB0aGFuICVzJy5mb3JtYXQoW3J1bGVzLmd0LCBydWxlcy5sdF0pIDogJycKzQEKDGZsb2F0Lmd0X2x0ZRq8AWhhcyhydWxlcy5sdGUpICYmIHJ1bGVzLmx0ZSA+PSBydWxlcy5ndCAmJiAodGhpcy5pc05hbigpIHx8IHRoaXMgPiBydWxlcy5sdGUgfHwgdGhpcyA8PSBydWxlcy5ndCk/ICdtdXN0IGJlIGdyZWF0ZXIgdGhhbiAlcyBhbmQgbGVzcyB0aGFuIG9yIGVxdWFsIHRvICVzJy5mb3JtYXQoW3J1bGVzLmd0LCBydWxlcy5sdGVdKSA6ICcnCtcBChZmbG9hdC5ndF9sdGVfZXhjbHVzaXZlGrwBaGFzKHJ1bGVzLmx0ZSkgJiYgcnVsZXMubHRlIDwgcnVsZXMuZ3QgJiYgKHRoaXMuaXNOYW4oKSB8fCAocnVsZXMubHRlIDwgdGhpcyAmJiB0aGlzIDw9IHJ1bGVzLmd0KSk/ICdtdXN0IGJlIGdyZWF0ZXIgdGhhbiAlcyBvciBsZXNzIHRoYW4gb3IgZXF1YWwgdG8gJXMnLmZvcm1hdChbcnVsZXMuZ3QsIHJ1bGVzLmx0ZV0pIDogJydIARKcCAoDZ3RlGAUgASgCQowIwkiICAqVAQoJZmxvYXQuZ3RlGocBIWhhcyhydWxlcy5sdCkgJiYgIWhhcyhydWxlcy5sdGUpICYmICh0aGlzLmlzTmFuKCkgfHwgdGhpcyA8IHJ1bGVzLmd0ZSk/ICdtdXN0IGJlIGdyZWF0ZXIgdGhhbiBvciBlcXVhbCB0byAlcycuZm9ybWF0KFtydWxlcy5ndGVdKSA6ICcnCswBCgxmbG9hdC5ndGVfbHQauwFoYXMocnVsZXMubHQpICYmIHJ1bGVzLmx0ID49IHJ1bGVzLmd0ZSAmJiAodGhpcy5pc05hbigpIHx8IHRoaXMgPj0gcnVsZXMubHQgfHwgdGhpcyA8IHJ1bGVzLmd0ZSk/ICdtdXN0IGJlIGdyZWF0ZXIgdGhhbiBvciBlcXVhbCB0byAlcyBhbmQgbGVzcyB0aGFuICVzJy5mb3JtYXQoW3J1bGVzLmd0ZSwgcnVsZXMubHRdKSA6ICcnCtYBChZmbG9hdC5ndGVfbHRfZXhjbHVzaXZlGrsBaGFzKHJ1bGVzLmx0KSAmJiBydWxlcy5sdCA8IHJ1bGVzLmd0ZSAmJiAodGhpcy5pc05hbigpIHx8IChydWxlcy5sdCA8PSB0aGlzICYmIHRoaXMgPCBydWxlcy5ndGUpKT8gJ211c3QgYmUgZ3JlYXRlciB0aGFuIG9yIGVxdWFsIHRvICVzIG9yIGxlc3MgdGhhbiAlcycuZm9ybWF0KFtydWxlcy5ndGUsIHJ1bGVzLmx0XSkgOiAnJwrcAQoNZmxvYXQuZ3RlX2x0ZRrKAWhhcyhydWxlcy5sdGUpICYmIHJ1bGVzLmx0ZSA+PSBydWxlcy5ndGUgJiYgKHRoaXMuaXNOYW4oKSB8fCB0aGlzID4gcnVsZXMubHRlIHx8IHRoaXMgPCBydWxlcy5ndGUpPyAnbXVzdCBiZSBncmVhdGVyIHRoYW4gb3IgZXF1YWwgdG8gJXMgYW5kIGxlc3MgdGhhbiBvciBlcXVhbCB0byAlcycuZm9ybWF0KFtydWxlcy5ndGUsIHJ1bGVzLmx0ZV0pIDogJycK5gEKF2Zsb2F0Lmd0ZV9sdGVfZXhjbHVzaXZlGsoBaGFzKHJ1bGVzLmx0ZSkgJiYgcnVsZXMubHRlIDwgcnVsZXMuZ3RlICYmICh0aGlzLmlzTmFuKCkgfHwgKHJ1bGVzLmx0ZSA8IHRoaXMgJiYgdGhpcyA8IHJ1bGVzLmd0ZSkpPyAnbXVzdCBiZSBncmVhdGVyIHRoYW4gb3IgZXF1YWwgdG8gJXMgb3IgbGVzcyB0aGFuIG9yIGVxdWFsIHRvICVzJy5mb3JtYXQoW3J1bGVzLmd0ZSwgcnVsZXMubHRlXSkgOiAnJ0gBEnkKAmluGAYgAygCQm3CSGoKaAoIZmxvYXQuaW4aXCEodGhpcyBpbiBnZXRGaWVsZChydWxlcywgJ2luJykpID8gJ211c3QgYmUgaW4gbGlzdCAlcycuZm9ybWF0KFtnZXRGaWVsZChydWxlcywgJ2luJyldKSA6ICcnEnAKBm5vdF9pbhgHIAMoAkJgwkhdClsKDGZsb2F0Lm5vdF9pbhpLdGhpcyBpbiBydWxlcy5ub3RfaW4gPyAnbXVzdCBub3QgYmUgaW4gbGlzdCAlcycuZm9ybWF0KFtydWxlcy5ub3RfaW5dKSA6ICcnEm8KBmZpbml0ZRgIIAEoCEJfwkhcCloKDGZsb2F0LmZpbml0ZRpKcnVsZXMuZmluaXRlID8gKHRoaXMuaXNOYW4oKSB8fCB0aGlzLmlzSW5mKCkgPyAnbXVzdCBiZSBmaW5pdGUnIDogJycpIDogJycSKwoHZXhhbXBsZRgJIAMoAkIawkgXChUKDWZsb2F0LmV4YW1wbGUaBHRydWUqCQjoBxCAgICAAkILCglsZXNzX3RoYW5CDgoMZ3JlYXRlcl90aGFuIooXCgtEb3VibGVSdWxlcxJ+CgVjb25zdBgBIAEoAUJvwkhsCmoKDGRvdWJsZS5jb25zdBpadGhpcyAhPSBnZXRGaWVsZChydWxlcywgJ2NvbnN0JykgPyAnbXVzdCBlcXVhbCAlcycuZm9ybWF0KFtnZXRGaWVsZChydWxlcywgJ2NvbnN0JyldKSA6ICcnEpoBCgJsdBgCIAEoAUKLAcJIhwEKhAEKCWRvdWJsZS5sdBp3IWhhcyhydWxlcy5ndGUpICYmICFoYXMocnVsZXMuZ3QpICYmICh0aGlzLmlzTmFuKCkgfHwgdGhpcyA+PSBydWxlcy5sdCk/ICdtdXN0IGJlIGxlc3MgdGhhbiAlcycuZm9ybWF0KFtydWxlcy5sdF0pIDogJydIABKqAQoDbHRlGAMgASgBQpoBwkiWAQqTAQoKZG91YmxlLmx0ZRqEASFoYXMocnVsZXMuZ3RlKSAmJiAhaGFzKHJ1bGVzLmd0KSAmJiAodGhpcy5pc05hbigpIHx8IHRoaXMgPiBydWxlcy5sdGUpPyAnbXVzdCBiZSBsZXNzIHRoYW4gb3IgZXF1YWwgdG8gJXMnLmZvcm1hdChbcnVsZXMubHRlXSkgOiAnJ0gAEtUHCgJndBgEIAEoAULGB8JIwgcKhwEKCWRvdWJsZS5ndBp6IWhhcyhydWxlcy5sdCkgJiYgIWhhcyhydWxlcy5sdGUpICYmICh0aGlzLmlzTmFuKCkgfHwgdGhpcyA8PSBydWxlcy5ndCk/ICdtdXN0IGJlIGdyZWF0ZXIgdGhhbiAlcycuZm9ybWF0KFtydWxlcy5ndF0pIDogJycKvgEKDGRvdWJsZS5ndF9sdBqtAWhhcyhydWxlcy5sdCkgJiYgcnVsZXMubHQgPj0gcnVsZXMuZ3QgJiYgKHRoaXMuaXNOYW4oKSB8fCB0aGlzID49IHJ1bGVzLmx0IHx8IHRoaXMgPD0gcnVsZXMuZ3QpPyAnbXVzdCBiZSBncmVhdGVyIHRoYW4gJXMgYW5kIGxlc3MgdGhhbiAlcycuZm9ybWF0KFtydWxlcy5ndCwgcnVsZXMubHRdKSA6ICcnCsgBChZkb3VibGUuZ3RfbHRfZXhjbHVzaXZlGq0BaGFzKHJ1bGVzLmx0KSAmJiBydWxlcy5sdCA8IHJ1bGVzLmd0ICYmICh0aGlzLmlzTmFuKCkgfHwgKHJ1bGVzLmx0IDw9IHRoaXMgJiYgdGhpcyA8PSBydWxlcy5ndCkpPyAnbXVzdCBiZSBncmVhdGVyIHRoYW4gJXMgb3IgbGVzcyB0aGFuICVzJy5mb3JtYXQoW3J1bGVzLmd0LCBydWxlcy5sdF0pIDogJycKzgEKDWRvdWJsZS5ndF9sdGUavAFoYXMocnVsZXMubHRlKSAmJiBydWxlcy5sdGUgPj0gcnVsZXMuZ3QgJiYgKHRoaXMuaXNOYW4oKSB8fCB0aGlzID4gcnVsZXMubHRlIHx8IHRoaXMgPD0gcnVsZXMuZ3QpPyAnbXVzdCBiZSBncmVhdGVyIHRoYW4gJXMgYW5kIGxlc3MgdGhhbiBvciBlcXVhbCB0byAlcycuZm9ybWF0KFtydWxlcy5ndCwgcnVsZXMubHRlXSkgOiAnJwrYAQoXZG91YmxlLmd0X2x0ZV9leGNsdXNpdmUavAFoYXMocnVsZXMubHRlKSAmJiBydWxlcy5sdGUgPCBydWxlcy5ndCAmJiAodGhpcy5pc05hbigpIHx8IChydWxlcy5sdGUgPCB0aGlzICYmIHRoaXMgPD0gcnVsZXMuZ3QpKT8gJ211c3QgYmUgZ3JlYXRlciB0aGFuICVzIG9yIGxlc3MgdGhhbiBvciBlcXVhbCB0byAlcycuZm9ybWF0KFtydWxlcy5ndCwgcnVsZXMubHRlXSkgOiAnJ0gBEqEICgNndGUYBSABKAFCkQjCSI0ICpYBCgpkb3VibGUuZ3RlGocBIWhhcyhydWxlcy5sdCkgJiYgIWhhcyhydWxlcy5sdGUpICYmICh0aGlzLmlzTmFuKCkgfHwgdGhpcyA8IHJ1bGVzLmd0ZSk/ICdtdXN0IGJlIGdyZWF0ZXIgdGhhbiBvciBlcXVhbCB0byAlcycuZm9ybWF0KFtydWxlcy5ndGVdKSA6ICcnCs0BCg1kb3VibGUuZ3RlX2x0GrsBaGFzKHJ1bGVzLmx0KSAmJiBydWxlcy5sdCA+PSBydWxlcy5ndGUgJiYgKHRoaXMuaXNOYW4oKSB8fCB0aGlzID49IHJ1bGVzLmx0IHx8IHRoaXMgPCBydWxlcy5ndGUpPyAnbXVzdCBiZSBncmVhdGVyIHRoYW4gb3IgZXF1YWwgdG8gJXMgYW5kIGxlc3MgdGhhbiAlcycuZm9ybWF0KFtydWxlcy5ndGUsIHJ1bGVzLmx0XSkgOiAnJwrXAQoXZG91YmxlLmd0ZV9sdF9leGNsdXNpdmUauwFoYXMocnVsZXMubHQpICYmIHJ1bGVzLmx0IDwgcnVsZXMuZ3RlICYmICh0aGlzLmlzTmFuKCkgfHwgKHJ1bGVzLmx0IDw9IHRoaXMgJiYgdGhpcyA8IHJ1bGVzLmd0ZSkpPyAnbXVzdCBiZSBncmVhdGVyIHRoYW4gb3IgZXF1YWwgdG8gJXMgb3IgbGVzcyB0aGFuICVzJy5mb3JtYXQoW3J1bGVzLmd0ZSwgcnVsZXMubHRdKSA6ICcnCt0BCg5kb3VibGUuZ3RlX2x0ZRrKAWhhcyhydWxlcy5sdGUpICYmIHJ1bGVzLmx0ZSA+PSBydWxlcy5ndGUgJiYgKHRoaXMuaXNOYW4oKSB8fCB0aGlzID4gcnVsZXMubHRlIHx8IHRoaXMgPCBydWxlcy5ndGUpPyAnbXVzdCBiZSBncmVhdGVyIHRoYW4gb3IgZXF1YWwgdG8gJXMgYW5kIGxlc3MgdGhhbiBvciBlcXVhbCB0byAlcycuZm9ybWF0KFtydWxlcy5ndGUsIHJ1bGVzLmx0ZV0pIDogJycK5wEKGGRvdWJsZS5ndGVfbHRlX2V4Y2x1c2l2ZRrKAWhhcyhydWxlcy5sdGUpICYmIHJ1bGVzLmx0ZSA8IHJ1bGVzLmd0ZSAmJiAodGhpcy5pc05hbigpIHx8IChydWxlcy5sdGUgPCB0aGlzICYmIHRoaXMgPCBydWxlcy5ndGUpKT8gJ211c3QgYmUgZ3JlYXRlciB0aGFuIG9yIGVxdWFsIHRvICVzIG9yIGxlc3MgdGhhbiBvciBlcXVhbCB0byAlcycuZm9ybWF0KFtydWxlcy5ndGUsIHJ1bGVzLmx0ZV0pIDogJydIARJ6CgJpbhgGIAMoAUJuwkhrCmkKCWRvdWJsZS5pbhpcISh0aGlzIGluIGdldEZpZWxkKHJ1bGVzLCAnaW4nKSkgPyAnbXVzdCBiZSBpbiBsaXN0ICVzJy5mb3JtYXQoW2dldEZpZWxkKHJ1bGVzLCAnaW4nKV0pIDogJycScQoGbm90X2luGAcgAygBQmHCSF4KXAoNZG91YmxlLm5vdF9pbhpLdGhpcyBpbiBydWxlcy5ub3RfaW4gPyAnbXVzdCBub3QgYmUgaW4gbGlzdCAlcycuZm9ybWF0KFtydWxlcy5ub3RfaW5dKSA6ICcnEnAKBmZpbml0ZRgIIAEoCEJgwkhdClsKDWRvdWJsZS5maW5pdGUaSnJ1bGVzLmZpbml0ZSA/ICh0aGlzLmlzTmFuKCkgfHwgdGhpcy5pc0luZigpID8gJ211c3QgYmUgZmluaXRlJyA6ICcnKSA6ICcnEiwKB2V4YW1wbGUYCSADKAFCG8JIGAoWCg5kb3VibGUuZXhhbXBsZRoEdHJ1ZSoJCOgHEICAgIACQgsKCWxlc3NfdGhhbkIOCgxncmVhdGVyX3RoYW4isBQKCkludDMyUnVsZXMSfQoFY29uc3QYASABKAVCbsJIawppCgtpbnQzMi5jb25zdBpadGhpcyAhPSBnZXRGaWVsZChydWxlcywgJ2NvbnN0JykgPyAnbXVzdCBlcXVhbCAlcycuZm9ybWF0KFtnZXRGaWVsZChydWxlcywgJ2NvbnN0JyldKSA6ICcnEoQBCgJsdBgCIAEoBUJ2wkhzCnEKCGludDMyLmx0GmUhaGFzKHJ1bGVzLmd0ZSkgJiYgIWhhcyhydWxlcy5ndCkgJiYgdGhpcyA+PSBydWxlcy5sdD8gJ211c3QgYmUgbGVzcyB0aGFuICVzJy5mb3JtYXQoW3J1bGVzLmx0XSkgOiAnJ0gAEpUBCgNsdGUYAyABKAVChQHCSIEBCn8KCWludDMyLmx0ZRpyIWhhcyhydWxlcy5ndGUpICYmICFoYXMocnVsZXMuZ3QpICYmIHRoaXMgPiBydWxlcy5sdGU/ICdtdXN0IGJlIGxlc3MgdGhhbiBvciBlcXVhbCB0byAlcycuZm9ybWF0KFtydWxlcy5sdGVdKSA6ICcnSAAS+QYKAmd0GAQgASgFQuoGwkjmBgp0CghpbnQzMi5ndBpoIWhhcyhydWxlcy5sdCkgJiYgIWhhcyhydWxlcy5sdGUpICYmIHRoaXMgPD0gcnVsZXMuZ3Q/ICdtdXN0IGJlIGdyZWF0ZXIgdGhhbiAlcycuZm9ybWF0KFtydWxlcy5ndF0pIDogJycKrQEKC2ludDMyLmd0X2x0Gp0BaGFzKHJ1bGVzLmx0KSAmJiBydWxlcy5sdCA+PSBydWxlcy5ndCAmJiAodGhpcyA+PSBydWxlcy5sdCB8fCB0aGlzIDw9IHJ1bGVzLmd0KT8gJ211c3QgYmUgZ3JlYXRlciB0aGFuICVzIGFuZCBsZXNzIHRoYW4gJXMnLmZvcm1hdChbcnVsZXMuZ3QsIHJ1bGVzLmx0XSkgOiAnJwq1AQoVaW50MzIuZ3RfbHRfZXhjbHVzaXZlGpsBaGFzKHJ1bGVzLmx0KSAmJiBydWxlcy5sdCA8IHJ1bGVzLmd0ICYmIChydWxlcy5sdCA8PSB0aGlzICYmIHRoaXMgPD0gcnVsZXMuZ3QpPyAnbXVzdCBiZSBncmVhdGVyIHRoYW4gJXMgb3IgbGVzcyB0aGFuICVzJy5mb3JtYXQoW3J1bGVzLmd0LCBydWxlcy5sdF0pIDogJycKvQEKDGludDMyLmd0X2x0ZRqsAWhhcyhydWxlcy5sdGUpICYmIHJ1bGVzLmx0ZSA+PSBydWxlcy5ndCAmJiAodGhpcyA+IHJ1bGVzLmx0ZSB8fCB0aGlzIDw9IHJ1bGVzLmd0KT8gJ211c3QgYmUgZ3JlYXRlciB0aGFuICVzIGFuZCBsZXNzIHRoYW4gb3IgZXF1YWwgdG8gJXMnLmZvcm1hdChbcnVsZXMuZ3QsIHJ1bGVzLmx0ZV0pIDogJycKxQEKFmludDMyLmd0X2x0ZV9leGNsdXNpdmUaqgFoYXMocnVsZXMubHRlKSAmJiBydWxlcy5sdGUgPCBydWxlcy5ndCAmJiAocnVsZXMubHRlIDwgdGhpcyAmJiB0aGlzIDw9IHJ1bGVzLmd0KT8gJ211c3QgYmUgZ3JlYXRlciB0aGFuICVzIG9yIGxlc3MgdGhhbiBvciBlcXVhbCB0byAlcycuZm9ybWF0KFtydWxlcy5ndCwgcnVsZXMubHRlXSkgOiAnJ0gBEsUHCgNndGUYBSABKAVCtQfCSLEHCoIBCglpbnQzMi5ndGUadSFoYXMocnVsZXMubHQpICYmICFoYXMocnVsZXMubHRlKSAmJiB0aGlzIDwgcnVsZXMuZ3RlPyAnbXVzdCBiZSBncmVhdGVyIHRoYW4gb3IgZXF1YWwgdG8gJXMnLmZvcm1hdChbcnVsZXMuZ3RlXSkgOiAnJwq8AQoMaW50MzIuZ3RlX2x0GqsBaGFzKHJ1bGVzLmx0KSAmJiBydWxlcy5sdCA+PSBydWxlcy5ndGUgJiYgKHRoaXMgPj0gcnVsZXMubHQgfHwgdGhpcyA8IHJ1bGVzLmd0ZSk/ICdtdXN0IGJlIGdyZWF0ZXIgdGhhbiBvciBlcXVhbCB0byAlcyBhbmQgbGVzcyB0aGFuICVzJy5mb3JtYXQoW3J1bGVzLmd0ZSwgcnVsZXMubHRdKSA6ICcnCsQBChZpbnQzMi5ndGVfbHRfZXhjbHVzaXZlGqkBaGFzKHJ1bGVzLmx0KSAmJiBydWxlcy5sdCA8IHJ1bGVzLmd0ZSAmJiAocnVsZXMubHQgPD0gdGhpcyAmJiB0aGlzIDwgcnVsZXMuZ3RlKT8gJ211c3QgYmUgZ3JlYXRlciB0aGFuIG9yIGVxdWFsIHRvICVzIG9yIGxlc3MgdGhhbiAlcycuZm9ybWF0KFtydWxlcy5ndGUsIHJ1bGVzLmx0XSkgOiAnJwrMAQoNaW50MzIuZ3RlX2x0ZRq6AWhhcyhydWxlcy5sdGUpICYmIHJ1bGVzLmx0ZSA+PSBydWxlcy5ndGUgJiYgKHRoaXMgPiBydWxlcy5sdGUgfHwgdGhpcyA8IHJ1bGVzLmd0ZSk/ICdtdXN0IGJlIGdyZWF0ZXIgdGhhbiBvciBlcXVhbCB0byAlcyBhbmQgbGVzcyB0aGFuIG9yIGVxdWFsIHRvICVzJy5mb3JtYXQoW3J1bGVzLmd0ZSwgcnVsZXMubHRlXSkgOiAnJwrUAQoXaW50MzIuZ3RlX2x0ZV9leGNsdXNpdmUauAFoYXMocnVsZXMubHRlKSAmJiBydWxlcy5sdGUgPCBydWxlcy5ndGUgJiYgKHJ1bGVzLmx0ZSA8IHRoaXMgJiYgdGhpcyA8IHJ1bGVzLmd0ZSk/ICdtdXN0IGJlIGdyZWF0ZXIgdGhhbiBvciBlcXVhbCB0byAlcyBvciBsZXNzIHRoYW4gb3IgZXF1YWwgdG8gJXMnLmZvcm1hdChbcnVsZXMuZ3RlLCBydWxlcy5sdGVdKSA6ICcnSAESeQoCaW4YBiADKAVCbcJIagpoCghpbnQzMi5pbhpcISh0aGlzIGluIGdldEZpZWxkKHJ1bGVzLCAnaW4nKSkgPyAnbXVzdCBiZSBpbiBsaXN0ICVzJy5mb3JtYXQoW2dldEZpZWxkKHJ1bGVzLCAnaW4nKV0pIDogJycScAoGbm90X2luGAcgAygFQmDCSF0KWwoMaW50MzIubm90X2luGkt0aGlzIGluIHJ1bGVzLm5vdF9pbiA/ICdtdXN0IG5vdCBiZSBpbiBsaXN0ICVzJy5mb3JtYXQoW3J1bGVzLm5vdF9pbl0pIDogJycSKwoHZXhhbXBsZRgIIAMoBUIawkgXChUKDWludDMyLmV4YW1wbGUaBHRydWUqCQjoBxCAgICAAkILCglsZXNzX3RoYW5CDgoMZ3JlYXRlcl90aGFuIrAUCgpJbnQ2NFJ1bGVzEn0KBWNvbnN0GAEgASgDQm7CSGsKaQoLaW50NjQuY29uc3QaWnRoaXMgIT0gZ2V0RmllbGQocnVsZXMsICdjb25zdCcpID8gJ211c3QgZXF1YWwgJXMnLmZvcm1hdChbZ2V0RmllbGQocnVsZXMsICdjb25zdCcpXSkgOiAnJxKEAQoCbHQYAiABKANCdsJIcwpxCghpbnQ2NC5sdBplIWhhcyhydWxlcy5ndGUpICYmICFoYXMocnVsZXMuZ3QpICYmIHRoaXMgPj0gcnVsZXMubHQ/ICdtdXN0IGJlIGxlc3MgdGhhbiAlcycuZm9ybWF0KFtydWxlcy5sdF0pIDogJydIABKVAQoDbHRlGAMgASgDQoUBwkiBAQp/CglpbnQ2NC5sdGUaciFoYXMocnVsZXMuZ3RlKSAmJiAhaGFzKHJ1bGVzLmd0KSAmJiB0aGlzID4gcnVsZXMubHRlPyAnbXVzdCBiZSBsZXNzIHRoYW4gb3IgZXF1YWwgdG8gJXMnLmZvcm1hdChbcnVsZXMubHRlXSkgOiAnJ0gAEvkGCgJndBgEIAEoA0LqBsJI5gYKdAoIaW50NjQuZ3QaaCFoYXMocnVsZXMubHQpICYmICFoYXMocnVsZXMubHRlKSAmJiB0aGlzIDw9IHJ1bGVzLmd0PyAnbXVzdCBiZSBncmVhdGVyIHRoYW4gJXMnLmZvcm1hdChbcnVsZXMuZ3RdKSA6ICcnCq0BCgtpbnQ2NC5ndF9sdBqdAWhhcyhydWxlcy5sdCkgJiYgcnVsZXMubHQgPj0gcnVsZXMuZ3QgJiYgKHRoaXMgPj0gcnVsZXMubHQgfHwgdGhpcyA8PSBydWxlcy5ndCk/ICdtdXN0IGJlIGdyZWF0ZXIgdGhhbiAlcyBhbmQgbGVzcyB0aGFuICVzJy5mb3JtYXQoW3J1bGVzLmd0LCBydWxlcy5sdF0pIDogJycKtQEKFWludDY0Lmd0X2x0X2V4Y2x1c2l2ZRqbAWhhcyhydWxlcy5sdCkgJiYgcnVsZXMubHQgPCBydWxlcy5ndCAmJiAocnVsZXMubHQgPD0gdGhpcyAmJiB0aGlzIDw9IHJ1bGVzLmd0KT8gJ211c3QgYmUgZ3JlYXRlciB0aGFuICVzIG9yIGxlc3MgdGhhbiAlcycuZm9ybWF0KFtydWxlcy5ndCwgcnVsZXMubHRdKSA6ICcnCr0BCgxpbnQ2NC5ndF9sdGUarAFoYXMocnVsZXMubHRlKSAmJiBydWxlcy5sdGUgPj0gcnVsZXMuZ3QgJiYgKHRoaXMgPiBydWxlcy5sdGUgfHwgdGhpcyA8PSBydWxlcy5ndCk/ICdtdXN0IGJlIGdyZWF0ZXIgdGhhbiAlcyBhbmQgbGVzcyB0aGFuIG9yIGVxdWFsIHRvICVzJy5mb3JtYXQoW3J1bGVzLmd0LCBydWxlcy5sdGVdKSA6ICcnCsUBChZpbnQ2NC5ndF9sdGVfZXhjbHVzaXZlGqoBaGFzKHJ1bGVzLmx0ZSkgJiYgcnVsZXMubHRlIDwgcnVsZXMuZ3QgJiYgKHJ1bGVzLmx0ZSA8IHRoaXMgJiYgdGhpcyA8PSBydWxlcy5ndCk/ICdtdXN0IGJlIGdyZWF0ZXIgdGhhbiAlcyBvciBsZXNzIHRoYW4gb3IgZXF1YWwgdG8gJXMnLmZvcm1hdChbcnVsZXMuZ3QsIHJ1bGVzLmx0ZV0pIDogJydIARLFBwoDZ3RlGAUgASgDQrUHwkixBwqCAQoJaW50NjQuZ3RlGnUhaGFzKHJ1bGVzLmx0KSAmJiAhaGFzKHJ1bGVzLmx0ZSkgJiYgdGhpcyA8IHJ1bGVzLmd0ZT8gJ211c3QgYmUgZ3JlYXRlciB0aGFuIG9yIGVxdWFsIHRvICVzJy5mb3JtYXQoW3J1bGVzLmd0ZV0pIDogJycKvAEKDGludDY0Lmd0ZV9sdBqrAWhhcyhydWxlcy5sdCkgJiYgcnVsZXMubHQgPj0gcnVsZXMuZ3RlICYmICh0aGlzID49IHJ1bGVzLmx0IHx8IHRoaXMgPCBydWxlcy5ndGUpPyAnbXVzdCBiZSBncmVhdGVyIHRoYW4gb3IgZXF1YWwgdG8gJXMgYW5kIGxlc3MgdGhhbiAlcycuZm9ybWF0KFtydWxlcy5ndGUsIHJ1bGVzLmx0XSkgOiAnJwrEAQoWaW50NjQuZ3RlX2x0X2V4Y2x1c2l2ZRqpAWhhcyhydWxlcy5sdCkgJiYgcnVsZXMubHQgPCBydWxlcy5ndGUgJiYgKHJ1bGVzLmx0IDw9IHRoaXMgJiYgdGhpcyA8IHJ1bGVzLmd0ZSk/ICdtdXN0IGJlIGdyZWF0ZXIgdGhhbiBvciBlcXVhbCB0byAlcyBvciBsZXNzIHRoYW4gJXMnLmZvcm1hdChbcnVsZXMuZ3RlLCBydWxlcy5sdF0pIDogJycKzAEKDWludDY0Lmd0ZV9sdGUaugFoYXMocnVsZXMubHRlKSAmJiBydWxlcy5sdGUgPj0gcnVsZXMuZ3RlICYmICh0aGlzID4gcnVsZXMubHRlIHx8IHRoaXMgPCBydWxlcy5ndGUpPyAnbXVzdCBiZSBncmVhdGVyIHRoYW4gb3IgZXF1YWwgdG8gJXMgYW5kIGxlc3MgdGhhbiBvciBlcXVhbCB0byAlcycuZm9ybWF0KFtydWxlcy5ndGUsIHJ1bGVzLmx0ZV0pIDogJycK1AEKF2ludDY0Lmd0ZV9sdGVfZXhjbHVzaXZlGrgBaGFzKHJ1bGVzLmx0ZSkgJiYgcnVsZXMubHRlIDwgcnVsZXMuZ3RlICYmIChydWxlcy5sdGUgPCB0aGlzICYmIHRoaXMgPCBydWxlcy5ndGUpPyAnbXVzdCBiZSBncmVhdGVyIHRoYW4gb3IgZXF1YWwgdG8gJXMgb3IgbGVzcyB0aGFuIG9yIGVxdWFsIHRvICVzJy5mb3JtYXQoW3J1bGVzLmd0ZSwgcnVsZXMubHRlXSkgOiAnJ0gBEnkKAmluGAYgAygDQm3CSGoKaAoIaW50NjQuaW4aXCEodGhpcyBpbiBnZXRGaWVsZChydWxlcywgJ2luJykpID8gJ211c3QgYmUgaW4gbGlzdCAlcycuZm9ybWF0KFtnZXRGaWVsZChydWxlcywgJ2luJyldKSA6ICcnEnAKBm5vdF9pbhgHIAMoA0JgwkhdClsKDGludDY0Lm5vdF9pbhpLdGhpcyBpbiBydWxlcy5ub3RfaW4gPyAnbXVzdCBub3QgYmUgaW4gbGlzdCAlcycuZm9ybWF0KFtydWxlcy5ub3RfaW5dKSA6ICcnEisKB2V4YW1wbGUYCSADKANCGsJIFwoVCg1pbnQ2NC5leGFtcGxlGgR0cnVlKgkI6AcQgICAgAJCCwoJbGVzc190aGFuQg4KDGdyZWF0ZXJfdGhhbiLCFAoLVUludDMyUnVsZXMSfgoFY29uc3QYASABKA1Cb8JIbApqCgx1aW50MzIuY29uc3QaWnRoaXMgIT0gZ2V0RmllbGQocnVsZXMsICdjb25zdCcpID8gJ211c3QgZXF1YWwgJXMnLmZvcm1hdChbZ2V0RmllbGQocnVsZXMsICdjb25zdCcpXSkgOiAnJxKFAQoCbHQYAiABKA1Cd8JIdApyCgl1aW50MzIubHQaZSFoYXMocnVsZXMuZ3RlKSAmJiAhaGFzKHJ1bGVzLmd0KSAmJiB0aGlzID49IHJ1bGVzLmx0PyAnbXVzdCBiZSBsZXNzIHRoYW4gJXMnLmZvcm1hdChbcnVsZXMubHRdKSA6ICcnSAASlwEKA2x0ZRgDIAEoDUKHAcJIgwEKgAEKCnVpbnQzMi5sdGUaciFoYXMocnVsZXMuZ3RlKSAmJiAhaGFzKHJ1bGVzLmd0KSAmJiB0aGlzID4gcnVsZXMubHRlPyAnbXVzdCBiZSBsZXNzIHRoYW4gb3IgZXF1YWwgdG8gJXMnLmZvcm1hdChbcnVsZXMubHRlXSkgOiAnJ0gAEv4GCgJndBgEIAEoDULvBsJI6wYKdQoJdWludDMyLmd0GmghaGFzKHJ1bGVzLmx0KSAmJiAhaGFzKHJ1bGVzLmx0ZSkgJiYgdGhpcyA8PSBydWxlcy5ndD8gJ211c3QgYmUgZ3JlYXRlciB0aGFuICVzJy5mb3JtYXQoW3J1bGVzLmd0XSkgOiAnJwquAQoMdWludDMyLmd0X2x0Gp0BaGFzKHJ1bGVzLmx0KSAmJiBydWxlcy5sdCA+PSBydWxlcy5ndCAmJiAodGhpcyA+PSBydWxlcy5sdCB8fCB0aGlzIDw9IHJ1bGVzLmd0KT8gJ211c3QgYmUgZ3JlYXRlciB0aGFuICVzIGFuZCBsZXNzIHRoYW4gJXMnLmZvcm1hdChbcnVsZXMuZ3QsIHJ1bGVzLmx0XSkgOiAnJwq2AQoWdWludDMyLmd0X2x0X2V4Y2x1c2l2ZRqbAWhhcyhydWxlcy5sdCkgJiYgcnVsZXMubHQgPCBydWxlcy5ndCAmJiAocnVsZXMubHQgPD0gdGhpcyAmJiB0aGlzIDw9IHJ1bGVzLmd0KT8gJ211c3QgYmUgZ3JlYXRlciB0aGFuICVzIG9yIGxlc3MgdGhhbiAlcycuZm9ybWF0KFtydWxlcy5ndCwgcnVsZXMubHRdKSA6ICcnCr4BCg11aW50MzIuZ3RfbHRlGqwBaGFzKHJ1bGVzLmx0ZSkgJiYgcnVsZXMubHRlID49IHJ1bGVzLmd0ICYmICh0aGlzID4gcnVsZXMubHRlIHx8IHRoaXMgPD0gcnVsZXMuZ3QpPyAnbXVzdCBiZSBncmVhdGVyIHRoYW4gJXMgYW5kIGxlc3MgdGhhbiBvciBlcXVhbCB0byAlcycuZm9ybWF0KFtydWxlcy5ndCwgcnVsZXMubHRlXSkgOiAnJwrGAQoXdWludDMyLmd0X2x0ZV9leGNsdXNpdmUaqgFoYXMocnVsZXMubHRlKSAmJiBydWxlcy5sdGUgPCBydWxlcy5ndCAmJiAocnVsZXMubHRlIDwgdGhpcyAmJiB0aGlzIDw9IHJ1bGVzLmd0KT8gJ211c3QgYmUgZ3JlYXRlciB0aGFuICVzIG9yIGxlc3MgdGhhbiBvciBlcXVhbCB0byAlcycuZm9ybWF0KFtydWxlcy5ndCwgcnVsZXMubHRlXSkgOiAnJ0gBEsoHCgNndGUYBSABKA1CugfCSLYHCoMBCgp1aW50MzIuZ3RlGnUhaGFzKHJ1bGVzLmx0KSAmJiAhaGFzKHJ1bGVzLmx0ZSkgJiYgdGhpcyA8IHJ1bGVzLmd0ZT8gJ211c3QgYmUgZ3JlYXRlciB0aGFuIG9yIGVxdWFsIHRvICVzJy5mb3JtYXQoW3J1bGVzLmd0ZV0pIDogJycKvQEKDXVpbnQzMi5ndGVfbHQaqwFoYXMocnVsZXMubHQpICYmIHJ1bGVzLmx0ID49IHJ1bGVzLmd0ZSAmJiAodGhpcyA+PSBydWxlcy5sdCB8fCB0aGlzIDwgcnVsZXMuZ3RlKT8gJ211c3QgYmUgZ3JlYXRlciB0aGFuIG9yIGVxdWFsIHRvICVzIGFuZCBsZXNzIHRoYW4gJXMnLmZvcm1hdChbcnVsZXMuZ3RlLCBydWxlcy5sdF0pIDogJycKxQEKF3VpbnQzMi5ndGVfbHRfZXhjbHVzaXZlGqkBaGFzKHJ1bGVzLmx0KSAmJiBydWxlcy5sdCA8IHJ1bGVzLmd0ZSAmJiAocnVsZXMubHQgPD0gdGhpcyAmJiB0aGlzIDwgcnVsZXMuZ3RlKT8gJ211c3QgYmUgZ3JlYXRlciB0aGFuIG9yIGVxdWFsIHRvICVzIG9yIGxlc3MgdGhhbiAlcycuZm9ybWF0KFtydWxlcy5ndGUsIHJ1bGVzLmx0XSkgOiAnJwrNAQoOdWludDMyLmd0ZV9sdGUaugFoYXMocnVsZXMubHRlKSAmJiBydWxlcy5sdGUgPj0gcnVsZXMuZ3RlICYmICh0aGlzID4gcnVsZXMubHRlIHx8IHRoaXMgPCBydWxlcy5ndGUpPyAnbXVzdCBiZSBncmVhdGVyIHRoYW4gb3IgZXF1YWwgdG8gJXMgYW5kIGxlc3MgdGhhbiBvciBlcXVhbCB0byAlcycuZm9ybWF0KFtydWxlcy5ndGUsIHJ1bGVzLmx0ZV0pIDogJycK1QEKGHVpbnQzMi5ndGVfbHRlX2V4Y2x1c2l2ZRq4AWhhcyhydWxlcy5sdGUpICYmIHJ1bGVzLmx0ZSA8IHJ1bGVzLmd0ZSAmJiAocnVsZXMubHRlIDwgdGhpcyAmJiB0aGlzIDwgcnVsZXMuZ3RlKT8gJ211c3QgYmUgZ3JlYXRlciB0aGFuIG9yIGVxdWFsIHRvICVzIG9yIGxlc3MgdGhhbiBvciBlcXVhbCB0byAlcycuZm9ybWF0KFtydWxlcy5ndGUsIHJ1bGVzLmx0ZV0pIDogJydIARJ6CgJpbhgGIAMoDUJuwkhrCmkKCXVpbnQzMi5pbhpcISh0aGlzIGluIGdldEZpZWxkKHJ1bGVzLCAnaW4nKSkgPyAnbXVzdCBiZSBpbiBsaXN0ICVzJy5mb3JtYXQoW2dldEZpZWxkKHJ1bGVzLCAnaW4nKV0pIDogJycScQoGbm90X2luGAcgAygNQmHCSF4KXAoNdWludDMyLm5vdF9pbhpLdGhpcyBpbiBydWxlcy5ub3RfaW4gPyAnbXVzdCBub3QgYmUgaW4gbGlzdCAlcycuZm9ybWF0KFtydWxlcy5ub3RfaW5dKSA6ICcnEiwKB2V4YW1wbGUYCCADKA1CG8JIGAoWCg51aW50MzIuZXhhbXBsZRoEdHJ1ZSoJCOgHEICAgIACQgsKCWxlc3NfdGhhbkIOCgxncmVhdGVyX3RoYW4iwhQKC1VJbnQ2NFJ1bGVzEn4KBWNvbnN0GAEgASgEQm/CSGwKagoMdWludDY0LmNvbnN0Glp0aGlzICE9IGdldEZpZWxkKHJ1bGVzLCAnY29uc3QnKSA/ICdtdXN0IGVxdWFsICVzJy5mb3JtYXQoW2dldEZpZWxkKHJ1bGVzLCAnY29uc3QnKV0pIDogJycShQEKAmx0GAIgASgEQnfCSHQKcgoJdWludDY0Lmx0GmUhaGFzKHJ1bGVzLmd0ZSkgJiYgIWhhcyhydWxlcy5ndCkgJiYgdGhpcyA+PSBydWxlcy5sdD8gJ211c3QgYmUgbGVzcyB0aGFuICVzJy5mb3JtYXQoW3J1bGVzLmx0XSkgOiAnJ0gAEpcBCgNsdGUYAyABKARChwHCSIMBCoABCgp1aW50NjQubHRlGnIhaGFzKHJ1bGVzLmd0ZSkgJiYgIWhhcyhydWxlcy5ndCkgJiYgdGhpcyA+IHJ1bGVzLmx0ZT8gJ211c3QgYmUgbGVzcyB0aGFuIG9yIGVxdWFsIHRvICVzJy5mb3JtYXQoW3J1bGVzLmx0ZV0pIDogJydIABL+BgoCZ3QYBCABKARC7wbCSOsGCnUKCXVpbnQ2NC5ndBpoIWhhcyhydWxlcy5sdCkgJiYgIWhhcyhydWxlcy5sdGUpICYmIHRoaXMgPD0gcnVsZXMuZ3Q/ICdtdXN0IGJlIGdyZWF0ZXIgdGhhbiAlcycuZm9ybWF0KFtydWxlcy5ndF0pIDogJycKrgEKDHVpbnQ2NC5ndF9sdBqdAWhhcyhydWxlcy5sdCkgJiYgcnVsZXMubHQgPj0gcnVsZXMuZ3QgJiYgKHRoaXMgPj0gcnVsZXMubHQgfHwgdGhpcyA8PSBydWxlcy5ndCk/ICdtdXN0IGJlIGdyZWF0ZXIgdGhhbiAlcyBhbmQgbGVzcyB0aGFuICVzJy5mb3JtYXQoW3J1bGVzLmd0LCBydWxlcy5sdF0pIDogJycKtgEKFnVpbnQ2NC5ndF9sdF9leGNsdXNpdmUamwFoYXMocnVsZXMubHQpICYmIHJ1bGVzLmx0IDwgcnVsZXMuZ3QgJiYgKHJ1bGVzLmx0IDw9IHRoaXMgJiYgdGhpcyA8PSBydWxlcy5ndCk/ICdtdXN0IGJlIGdyZWF0ZXIgdGhhbiAlcyBvciBsZXNzIHRoYW4gJXMnLmZvcm1hdChbcnVsZXMuZ3QsIHJ1bGVzLmx0XSkgOiAnJwq+AQoNdWludDY0Lmd0X2x0ZRqsAWhhcyhydWxlcy5sdGUpICYmIHJ1bGVzLmx0ZSA+PSBydWxlcy5ndCAmJiAodGhpcyA+IHJ1bGVzLmx0ZSB8fCB0aGlzIDw9IHJ1bGVzLmd0KT8gJ211c3QgYmUgZ3JlYXRlciB0aGFuICVzIGFuZCBsZXNzIHRoYW4gb3IgZXF1YWwgdG8gJXMnLmZvcm1hdChbcnVsZXMuZ3QsIHJ1bGVzLmx0ZV0pIDogJycKxgEKF3VpbnQ2NC5ndF9sdGVfZXhjbHVzaXZlGqoBaGFzKHJ1bGVzLmx0ZSkgJiYgcnVsZXMubHRlIDwgcnVsZXMuZ3QgJiYgKHJ1bGVzLmx0ZSA8IHRoaXMgJiYgdGhpcyA8PSBydWxlcy5ndCk/ICdtdXN0IGJlIGdyZWF0ZXIgdGhhbiAlcyBvciBsZXNzIHRoYW4gb3IgZXF1YWwgdG8gJXMnLmZvcm1hdChbcnVsZXMuZ3QsIHJ1bGVzLmx0ZV0pIDogJydIARLKBwoDZ3RlGAUgASgEQroHwki2BwqDAQoKdWludDY0Lmd0ZRp1IWhhcyhydWxlcy5sdCkgJiYgIWhhcyhydWxlcy5sdGUpICYmIHRoaXMgPCBydWxlcy5ndGU/ICdtdXN0IGJlIGdyZWF0ZXIgdGhhbiBvciBlcXVhbCB0byAlcycuZm9ybWF0KFtydWxlcy5ndGVdKSA6ICcnCr0BCg11aW50NjQuZ3RlX2x0GqsBaGFzKHJ1bGVzLmx0KSAmJiBydWxlcy5sdCA+PSBydWxlcy5ndGUgJiYgKHRoaXMgPj0gcnVsZXMubHQgfHwgdGhpcyA8IHJ1bGVzLmd0ZSk/ICdtdXN0IGJlIGdyZWF0ZXIgdGhhbiBvciBlcXVhbCB0byAlcyBhbmQgbGVzcyB0aGFuICVzJy5mb3JtYXQoW3J1bGVzLmd0ZSwgcnVsZXMubHRdKSA6ICcnCsUBChd1aW50NjQuZ3RlX2x0X2V4Y2x1c2l2ZRqpAWhhcyhydWxlcy5sdCkgJiYgcnVsZXMubHQgPCBydWxlcy5ndGUgJiYgKHJ1bGVzLmx0IDw9IHRoaXMgJiYgdGhpcyA8IHJ1bGVzLmd0ZSk/ICdtdXN0IGJlIGdyZWF0ZXIgdGhhbiBvciBlcXVhbCB0byAlcyBvciBsZXNzIHRoYW4gJXMnLmZvcm1hdChbcnVsZXMuZ3RlLCBydWxlcy5sdF0pIDogJycKzQEKDnVpbnQ2NC5ndGVfbHRlGroBaGFzKHJ1bGVzLmx0ZSkgJiYgcnVsZXMubHRlID49IHJ1bGVzLmd0ZSAmJiAodGhpcyA+IHJ1bGVzLmx0ZSB8fCB0aGlzIDwgcnVsZXMuZ3RlKT8gJ211c3QgYmUgZ3JlYXRlciB0aGFuIG9yIGVxdWFsIHRvICVzIGFuZCBsZXNzIHRoYW4gb3IgZXF1YWwgdG8gJXMnLmZvcm1hdChbcnVsZXMuZ3RlLCBydWxlcy5sdGVdKSA6ICcnCtUBChh1aW50NjQuZ3RlX2x0ZV9leGNsdXNpdmUauAFoYXMocnVsZXMubHRlKSAmJiBydWxlcy5sdGUgPCBydWxlcy5ndGUgJiYgKHJ1bGVzLmx0ZSA8IHRoaXMgJiYgdGhpcyA8IHJ1bGVzLmd0ZSk/ICdtdXN0IGJlIGdyZWF0ZXIgdGhhbiBvciBlcXVhbCB0byAlcyBvciBsZXNzIHRoYW4gb3IgZXF1YWwgdG8gJXMnLmZvcm1hdChbcnVsZXMuZ3RlLCBydWxlcy5sdGVdKSA6ICcnSAESegoCaW4YBiADKARCbsJIawppCgl1aW50NjQuaW4aXCEodGhpcyBpbiBnZXRGaWVsZChydWxlcywgJ2luJykpID8gJ211c3QgYmUgaW4gbGlzdCAlcycuZm9ybWF0KFtnZXRGaWVsZChydWxlcywgJ2luJyldKSA6ICcnEnEKBm5vdF9pbhgHIAMoBEJhwkheClwKDXVpbnQ2NC5ub3RfaW4aS3RoaXMgaW4gcnVsZXMubm90X2luID8gJ211c3Qgbm90IGJlIGluIGxpc3QgJXMnLmZvcm1hdChbcnVsZXMubm90X2luXSkgOiAnJxIsCgdleGFtcGxlGAggAygEQhvCSBgKFgoOdWludDY0LmV4YW1wbGUaBHRydWUqCQjoBxCAgICAAkILCglsZXNzX3RoYW5CDgoMZ3JlYXRlcl90aGFuIsIUCgtTSW50MzJSdWxlcxJ+CgVjb25zdBgBIAEoEUJvwkhsCmoKDHNpbnQzMi5jb25zdBpadGhpcyAhPSBnZXRGaWVsZChydWxlcywgJ2NvbnN0JykgPyAnbXVzdCBlcXVhbCAlcycuZm9ybWF0KFtnZXRGaWVsZChydWxlcywgJ2NvbnN0JyldKSA6ICcnEoUBCgJsdBgCIAEoEUJ3wkh0CnIKCXNpbnQzMi5sdBplIWhhcyhydWxlcy5ndGUpICYmICFoYXMocnVsZXMuZ3QpICYmIHRoaXMgPj0gcnVsZXMubHQ/ICdtdXN0IGJlIGxlc3MgdGhhbiAlcycuZm9ybWF0KFtydWxlcy5sdF0pIDogJydIABKXAQoDbHRlGAMgASgRQocBwkiDAQqAAQoKc2ludDMyLmx0ZRpyIWhhcyhydWxlcy5ndGUpICYmICFoYXMocnVsZXMuZ3QpICYmIHRoaXMgPiBydWxlcy5sdGU/ICdtdXN0IGJlIGxlc3MgdGhhbiBvciBlcXVhbCB0byAlcycuZm9ybWF0KFtydWxlcy5sdGVdKSA6ICcnSAAS/gYKAmd0GAQgASgRQu8GwkjrBgp1CglzaW50MzIuZ3QaaCFoYXMocnVsZXMubHQpICYmICFoYXMocnVsZXMubHRlKSAmJiB0aGlzIDw9IHJ1bGVzLmd0PyAnbXVzdCBiZSBncmVhdGVyIHRoYW4gJXMnLmZvcm1hdChbcnVsZXMuZ3RdKSA6ICcnCq4BCgxzaW50MzIuZ3RfbHQanQFoYXMocnVsZXMubHQpICYmIHJ1bGVzLmx0ID49IHJ1bGVzLmd0ICYmICh0aGlzID49IHJ1bGVzLmx0IHx8IHRoaXMgPD0gcnVsZXMuZ3QpPyAnbXVzdCBiZSBncmVhdGVyIHRoYW4gJXMgYW5kIGxlc3MgdGhhbiAlcycuZm9ybWF0KFtydWxlcy5ndCwgcnVsZXMubHRdKSA6ICcnCrYBChZzaW50MzIuZ3RfbHRfZXhjbHVzaXZlGpsBaGFzKHJ1bGVzLmx0KSAmJiBydWxlcy5sdCA8IHJ1bGVzLmd0ICYmIChydWxlcy5sdCA8PSB0aGlzICYmIHRoaXMgPD0gcnVsZXMuZ3QpPyAnbXVzdCBiZSBncmVhdGVyIHRoYW4gJXMgb3IgbGVzcyB0aGFuICVzJy5mb3JtYXQoW3J1bGVzLmd0LCBydWxlcy5sdF0pIDogJycKvgEKDXNpbnQzMi5ndF9sdGUarAFoYXMocnVsZXMubHRlKSAmJiBydWxlcy5sdGUgPj0gcnVsZXMuZ3QgJiYgKHRoaXMgPiBydWxlcy5sdGUgfHwgdGhpcyA8PSBydWxlcy5ndCk/ICdtdXN0IGJlIGdyZWF0ZXIgdGhhbiAlcyBhbmQgbGVzcyB0aGFuIG9yIGVxdWFsIHRvICVzJy5mb3JtYXQoW3J1bGVzLmd0LCBydWxlcy5sdGVdKSA6ICcnCsYBChdzaW50MzIuZ3RfbHRlX2V4Y2x1c2l2ZRqqAWhhcyhydWxlcy5sdGUpICYmIHJ1bGVzLmx0ZSA8IHJ1bGVzLmd0ICYmIChydWxlcy5sdGUgPCB0aGlzICYmIHRoaXMgPD0gcnVsZXMuZ3QpPyAnbXVzdCBiZSBncmVhdGVyIHRoYW4gJXMgb3IgbGVzcyB0aGFuIG9yIGVxdWFsIHRvICVzJy5mb3JtYXQoW3J1bGVzLmd0LCBydWxlcy5sdGVdKSA6ICcnSAESygcKA2d0ZRgFIAEoEUK6B8JItgcKgwEKCnNpbnQzMi5ndGUadSFoYXMocnVsZXMubHQpICYmICFoYXMocnVsZXMubHRlKSAmJiB0aGlzIDwgcnVsZXMuZ3RlPyAnbXVzdCBiZSBncmVhdGVyIHRoYW4gb3IgZXF1YWwgdG8gJXMnLmZvcm1hdChbcnVsZXMuZ3RlXSkgOiAnJwq9AQoNc2ludDMyLmd0ZV9sdBqrAWhhcyhydWxlcy5sdCkgJiYgcnVsZXMubHQgPj0gcnVsZXMuZ3RlICYmICh0aGlzID49IHJ1bGVzLmx0IHx8IHRoaXMgPCBydWxlcy5ndGUpPyAnbXVzdCBiZSBncmVhdGVyIHRoYW4gb3IgZXF1YWwgdG8gJXMgYW5kIGxlc3MgdGhhbiAlcycuZm9ybWF0KFtydWxlcy5ndGUsIHJ1bGVzLmx0XSkgOiAnJwrFAQoXc2ludDMyLmd0ZV9sdF9leGNsdXNpdmUaqQFoYXMocnVsZXMubHQpICYmIHJ1bGVzLmx0IDwgcnVsZXMuZ3RlICYmIChydWxlcy5sdCA8PSB0aGlzICYmIHRoaXMgPCBydWxlcy5ndGUpPyAnbXVzdCBiZSBncmVhdGVyIHRoYW4gb3IgZXF1YWwgdG8gJXMgb3IgbGVzcyB0aGFuICVzJy5mb3JtYXQoW3J1bGVzLmd0ZSwgcnVsZXMubHRdKSA6ICcnCs0BCg5zaW50MzIuZ3RlX2x0ZRq6AWhhcyhydWxlcy5sdGUpICYmIHJ1bGVzLmx0ZSA+PSBydWxlcy5ndGUgJiYgKHRoaXMgPiBydWxlcy5sdGUgfHwgdGhpcyA8IHJ1bGVzLmd0ZSk/ICdtdXN0IGJlIGdyZWF0ZXIgdGhhbiBvciBlcXVhbCB0byAlcyBhbmQgbGVzcyB0aGFuIG9yIGVxdWFsIHRvICVzJy5mb3JtYXQoW3J1bGVzLmd0ZSwgcnVsZXMubHRlXSkgOiAnJwrVAQoYc2ludDMyLmd0ZV9sdGVfZXhjbHVzaXZlGrgBaGFzKHJ1bGVzLmx0ZSkgJiYgcnVsZXMubHRlIDwgcnVsZXMuZ3RlICYmIChydWxlcy5sdGUgPCB0aGlzICYmIHRoaXMgPCBydWxlcy5ndGUpPyAnbXVzdCBiZSBncmVhdGVyIHRoYW4gb3IgZXF1YWwgdG8gJXMgb3IgbGVzcyB0aGFuIG9yIGVxdWFsIHRvICVzJy5mb3JtYXQoW3J1bGVzLmd0ZSwgcnVsZXMubHRlXSkgOiAnJ0gBEnoKAmluGAYgAygRQm7CSGsKaQoJc2ludDMyLmluGlwhKHRoaXMgaW4gZ2V0RmllbGQocnVsZXMsICdpbicpKSA/ICdtdXN0IGJlIGluIGxpc3QgJXMnLmZvcm1hdChbZ2V0RmllbGQocnVsZXMsICdpbicpXSkgOiAnJxJxCgZub3RfaW4YByADKBFCYcJIXgpcCg1zaW50MzIubm90X2luGkt0aGlzIGluIHJ1bGVzLm5vdF9pbiA/ICdtdXN0IG5vdCBiZSBpbiBsaXN0ICVzJy5mb3JtYXQoW3J1bGVzLm5vdF9pbl0pIDogJycSLAoHZXhhbXBsZRgIIAMoEUIbwkgYChYKDnNpbnQzMi5leGFtcGxlGgR0cnVlKgkI6AcQgICAgAJCCwoJbGVzc190aGFuQg4KDGdyZWF0ZXJfdGhhbiLCFAoLU0ludDY0UnVsZXMSfgoFY29uc3QYASABKBJCb8JIbApqCgxzaW50NjQuY29uc3QaWnRoaXMgIT0gZ2V0RmllbGQocnVsZXMsICdjb25zdCcpID8gJ211c3QgZXF1YWwgJXMnLmZvcm1hdChbZ2V0RmllbGQocnVsZXMsICdjb25zdCcpXSkgOiAnJxKFAQoCbHQYAiABKBJCd8JIdApyCglzaW50NjQubHQaZSFoYXMocnVsZXMuZ3RlKSAmJiAhaGFzKHJ1bGVzLmd0KSAmJiB0aGlzID49IHJ1bGVzLmx0PyAnbXVzdCBiZSBsZXNzIHRoYW4gJXMnLmZvcm1hdChbcnVsZXMubHRdKSA6ICcnSAASlwEKA2x0ZRgDIAEoEkKHAcJIgwEKgAEKCnNpbnQ2NC5sdGUaciFoYXMocnVsZXMuZ3RlKSAmJiAhaGFzKHJ1bGVzLmd0KSAmJiB0aGlzID4gcnVsZXMubHRlPyAnbXVzdCBiZSBsZXNzIHRoYW4gb3IgZXF1YWwgdG8gJXMnLmZvcm1hdChbcnVsZXMubHRlXSkgOiAnJ0gAEv4GCgJndBgEIAEoEkLvBsJI6wYKdQoJc2ludDY0Lmd0GmghaGFzKHJ1bGVzLmx0KSAmJiAhaGFzKHJ1bGVzLmx0ZSkgJiYgdGhpcyA8PSBydWxlcy5ndD8gJ211c3QgYmUgZ3JlYXRlciB0aGFuICVzJy5mb3JtYXQoW3J1bGVzLmd0XSkgOiAnJwquAQoMc2ludDY0Lmd0X2x0Gp0BaGFzKHJ1bGVzLmx0KSAmJiBydWxlcy5sdCA+PSBydWxlcy5ndCAmJiAodGhpcyA+PSBydWxlcy5sdCB8fCB0aGlzIDw9IHJ1bGVzLmd0KT8gJ211c3QgYmUgZ3JlYXRlciB0aGFuICVzIGFuZCBsZXNzIHRoYW4gJXMnLmZvcm1hdChbcnVsZXMuZ3QsIHJ1bGVzLmx0XSkgOiAnJwq2AQoWc2ludDY0Lmd0X2x0X2V4Y2x1c2l2ZRqbAWhhcyhydWxlcy5sdCkgJiYgcnVsZXMubHQgPCBydWxlcy5ndCAmJiAocnVsZXMubHQgPD0gdGhpcyAmJiB0aGlzIDw9IHJ1bGVzLmd0KT8gJ211c3QgYmUgZ3JlYXRlciB0aGFuICVzIG9yIGxlc3MgdGhhbiAlcycuZm9ybWF0KFtydWxlcy5ndCwgcnVsZXMubHRdKSA6ICcnCr4BCg1zaW50NjQuZ3RfbHRlGqwBaGFzKHJ1bGVzLmx0ZSkgJiYgcnVsZXMubHRlID49IHJ1bGVzLmd0ICYmICh0aGlzID4gcnVsZXMubHRlIHx8IHRoaXMgPD0gcnVsZXMuZ3QpPyAnbXVzdCBiZSBncmVhdGVyIHRoYW4gJXMgYW5kIGxlc3MgdGhhbiBvciBlcXVhbCB0byAlcycuZm9ybWF0KFtydWxlcy5ndCwgcnVsZXMubHRlXSkgOiAnJwrGAQoXc2ludDY0Lmd0X2x0ZV9leGNsdXNpdmUaqgFoYXMocnVsZXMubHRlKSAmJiBydWxlcy5sdGUgPCBydWxlcy5ndCAmJiAocnVsZXMubHRlIDwgdGhpcyAmJiB0aGlzIDw9IHJ1bGVzLmd0KT8gJ211c3QgYmUgZ3JlYXRlciB0aGFuICVzIG9yIGxlc3MgdGhhbiBvciBlcXVhbCB0byAlcycuZm9ybWF0KFtydWxlcy5ndCwgcnVsZXMubHRlXSkgOiAnJ0gBEsoHCgNndGUYBSABKBJCugfCSLYHCoMBCgpzaW50NjQuZ3RlGnUhaGFzKHJ1bGVzLmx0KSAmJiAhaGFzKHJ1bGVzLmx0ZSkgJiYgdGhpcyA8IHJ1bGVzLmd0ZT8gJ211c3QgYmUgZ3JlYXRlciB0aGFuIG9yIGVxdWFsIHRvICVzJy5mb3JtYXQoW3J1bGVzLmd0ZV0pIDogJycKvQEKDXNpbnQ2NC5ndGVfbHQaqwFoYXMocnVsZXMubHQpICYmIHJ1bGVzLmx0ID49IHJ1bGVzLmd0ZSAmJiAodGhpcyA+PSBydWxlcy5sdCB8fCB0aGlzIDwgcnVsZXMuZ3RlKT8gJ211c3QgYmUgZ3JlYXRlciB0aGFuIG9yIGVxdWFsIHRvICVzIGFuZCBsZXNzIHRoYW4gJXMnLmZvcm1hdChbcnVsZXMuZ3RlLCBydWxlcy5sdF0pIDogJycKxQEKF3NpbnQ2NC5ndGVfbHRfZXhjbHVzaXZlGqkBaGFzKHJ1bGVzLmx0KSAmJiBydWxlcy5sdCA8IHJ1bGVzLmd0ZSAmJiAocnVsZXMubHQgPD0gdGhpcyAmJiB0aGlzIDwgcnVsZXMuZ3RlKT8gJ211c3QgYmUgZ3JlYXRlciB0aGFuIG9yIGVxdWFsIHRvICVzIG9yIGxlc3MgdGhhbiAlcycuZm9ybWF0KFtydWxlcy5ndGUsIHJ1bGVzLmx0XSkgOiAnJwrNAQoOc2ludDY0Lmd0ZV9sdGUaugFoYXMocnVsZXMubHRlKSAmJiBydWxlcy5sdGUgPj0gcnVsZXMuZ3RlICYmICh0aGlzID4gcnVsZXMubHRlIHx8IHRoaXMgPCBydWxlcy5ndGUpPyAnbXVzdCBiZSBncmVhdGVyIHRoYW4gb3IgZXF1YWwgdG8gJXMgYW5kIGxlc3MgdGhhbiBvciBlcXVhbCB0byAlcycuZm9ybWF0KFtydWxlcy5ndGUsIHJ1bGVzLmx0ZV0pIDogJycK1QEKGHNpbnQ2NC5ndGVfbHRlX2V4Y2x1c2l2ZRq4AWhhcyhydWxlcy5sdGUpICYmIHJ1bGVzLmx0ZSA8IHJ1bGVzLmd0ZSAmJiAocnVsZXMubHRlIDwgdGhpcyAmJiB0aGlzIDwgcnVsZXMuZ3RlKT8gJ211c3QgYmUgZ3JlYXRlciB0aGFuIG9yIGVxdWFsIHRvICVzIG9yIGxlc3MgdGhhbiBvciBlcXVhbCB0byAlcycuZm9ybWF0KFtydWxlcy5ndGUsIHJ1bGVzLmx0ZV0pIDogJydIARJ6CgJpbhgGIAMoEkJuwkhrCmkKCXNpbnQ2NC5pbhpcISh0aGlzIGluIGdldEZpZWxkKHJ1bGVzLCAnaW4nKSkgPyAnbXVzdCBiZSBpbiBsaXN0ICVzJy5mb3JtYXQoW2dldEZpZWxkKHJ1bGVzLCAnaW4nKV0pIDogJycScQoGbm90X2luGAcgAygSQmHCSF4KXAoNc2ludDY0Lm5vdF9pbhpLdGhpcyBpbiBydWxlcy5ub3RfaW4gPyAnbXVzdCBub3QgYmUgaW4gbGlzdCAlcycuZm9ybWF0KFtydWxlcy5ub3RfaW5dKSA6ICcnEiwKB2V4YW1wbGUYCCADKBJCG8JIGAoWCg5zaW50NjQuZXhhbXBsZRoEdHJ1ZSoJCOgHEICAgIACQgsKCWxlc3NfdGhhbkIOCgxncmVhdGVyX3RoYW4i0xQKDEZpeGVkMzJSdWxlcxJ/CgVjb25zdBgBIAEoB0JwwkhtCmsKDWZpeGVkMzIuY29uc3QaWnRoaXMgIT0gZ2V0RmllbGQocnVsZXMsICdjb25zdCcpID8gJ211c3QgZXF1YWwgJXMnLmZvcm1hdChbZ2V0RmllbGQocnVsZXMsICdjb25zdCcpXSkgOiAnJxKGAQoCbHQYAiABKAdCeMJIdQpzCgpmaXhlZDMyLmx0GmUhaGFzKHJ1bGVzLmd0ZSkgJiYgIWhhcyhydWxlcy5ndCkgJiYgdGhpcyA+PSBydWxlcy5sdD8gJ211c3QgYmUgbGVzcyB0aGFuICVzJy5mb3JtYXQoW3J1bGVzLmx0XSkgOiAnJ0gAEpgBCgNsdGUYAyABKAdCiAHCSIQBCoEBCgtmaXhlZDMyLmx0ZRpyIWhhcyhydWxlcy5ndGUpICYmICFoYXMocnVsZXMuZ3QpICYmIHRoaXMgPiBydWxlcy5sdGU/ICdtdXN0IGJlIGxlc3MgdGhhbiBvciBlcXVhbCB0byAlcycuZm9ybWF0KFtydWxlcy5sdGVdKSA6ICcnSAASgwcKAmd0GAQgASgHQvQGwkjwBgp2CgpmaXhlZDMyLmd0GmghaGFzKHJ1bGVzLmx0KSAmJiAhaGFzKHJ1bGVzLmx0ZSkgJiYgdGhpcyA8PSBydWxlcy5ndD8gJ211c3QgYmUgZ3JlYXRlciB0aGFuICVzJy5mb3JtYXQoW3J1bGVzLmd0XSkgOiAnJwqvAQoNZml4ZWQzMi5ndF9sdBqdAWhhcyhydWxlcy5sdCkgJiYgcnVsZXMubHQgPj0gcnVsZXMuZ3QgJiYgKHRoaXMgPj0gcnVsZXMubHQgfHwgdGhpcyA8PSBydWxlcy5ndCk/ICdtdXN0IGJlIGdyZWF0ZXIgdGhhbiAlcyBhbmQgbGVzcyB0aGFuICVzJy5mb3JtYXQoW3J1bGVzLmd0LCBydWxlcy5sdF0pIDogJycKtwEKF2ZpeGVkMzIuZ3RfbHRfZXhjbHVzaXZlGpsBaGFzKHJ1bGVzLmx0KSAmJiBydWxlcy5sdCA8IHJ1bGVzLmd0ICYmIChydWxlcy5sdCA8PSB0aGlzICYmIHRoaXMgPD0gcnVsZXMuZ3QpPyAnbXVzdCBiZSBncmVhdGVyIHRoYW4gJXMgb3IgbGVzcyB0aGFuICVzJy5mb3JtYXQoW3J1bGVzLmd0LCBydWxlcy5sdF0pIDogJycKvwEKDmZpeGVkMzIuZ3RfbHRlGqwBaGFzKHJ1bGVzLmx0ZSkgJiYgcnVsZXMubHRlID49IHJ1bGVzLmd0ICYmICh0aGlzID4gcnVsZXMubHRlIHx8IHRoaXMgPD0gcnVsZXMuZ3QpPyAnbXVzdCBiZSBncmVhdGVyIHRoYW4gJXMgYW5kIGxlc3MgdGhhbiBvciBlcXVhbCB0byAlcycuZm9ybWF0KFtydWxlcy5ndCwgcnVsZXMubHRlXSkgOiAnJwrHAQoYZml4ZWQzMi5ndF9sdGVfZXhjbHVzaXZlGqoBaGFzKHJ1bGVzLmx0ZSkgJiYgcnVsZXMubHRlIDwgcnVsZXMuZ3QgJiYgKHJ1bGVzLmx0ZSA8IHRoaXMgJiYgdGhpcyA8PSBydWxlcy5ndCk/ICdtdXN0IGJlIGdyZWF0ZXIgdGhhbiAlcyBvciBsZXNzIHRoYW4gb3IgZXF1YWwgdG8gJXMnLmZvcm1hdChbcnVsZXMuZ3QsIHJ1bGVzLmx0ZV0pIDogJydIARLPBwoDZ3RlGAUgASgHQr8Hwki7BwqEAQoLZml4ZWQzMi5ndGUadSFoYXMocnVsZXMubHQpICYmICFoYXMocnVsZXMubHRlKSAmJiB0aGlzIDwgcnVsZXMuZ3RlPyAnbXVzdCBiZSBncmVhdGVyIHRoYW4gb3IgZXF1YWwgdG8gJXMnLmZvcm1hdChbcnVsZXMuZ3RlXSkgOiAnJwq+AQoOZml4ZWQzMi5ndGVfbHQaqwFoYXMocnVsZXMubHQpICYmIHJ1bGVzLmx0ID49IHJ1bGVzLmd0ZSAmJiAodGhpcyA+PSBydWxlcy5sdCB8fCB0aGlzIDwgcnVsZXMuZ3RlKT8gJ211c3QgYmUgZ3JlYXRlciB0aGFuIG9yIGVxdWFsIHRvICVzIGFuZCBsZXNzIHRoYW4gJXMnLmZvcm1hdChbcnVsZXMuZ3RlLCBydWxlcy5sdF0pIDogJycKxgEKGGZpeGVkMzIuZ3RlX2x0X2V4Y2x1c2l2ZRqpAWhhcyhydWxlcy5sdCkgJiYgcnVsZXMubHQgPCBydWxlcy5ndGUgJiYgKHJ1bGVzLmx0IDw9IHRoaXMgJiYgdGhpcyA8IHJ1bGVzLmd0ZSk/ICdtdXN0IGJlIGdyZWF0ZXIgdGhhbiBvciBlcXVhbCB0byAlcyBvciBsZXNzIHRoYW4gJXMnLmZvcm1hdChbcnVsZXMuZ3RlLCBydWxlcy5sdF0pIDogJycKzgEKD2ZpeGVkMzIuZ3RlX2x0ZRq6AWhhcyhydWxlcy5sdGUpICYmIHJ1bGVzLmx0ZSA+PSBydWxlcy5ndGUgJiYgKHRoaXMgPiBydWxlcy5sdGUgfHwgdGhpcyA8IHJ1bGVzLmd0ZSk/ICdtdXN0IGJlIGdyZWF0ZXIgdGhhbiBvciBlcXVhbCB0byAlcyBhbmQgbGVzcyB0aGFuIG9yIGVxdWFsIHRvICVzJy5mb3JtYXQoW3J1bGVzLmd0ZSwgcnVsZXMubHRlXSkgOiAnJwrWAQoZZml4ZWQzMi5ndGVfbHRlX2V4Y2x1c2l2ZRq4AWhhcyhydWxlcy5sdGUpICYmIHJ1bGVzLmx0ZSA8IHJ1bGVzLmd0ZSAmJiAocnVsZXMubHRlIDwgdGhpcyAmJiB0aGlzIDwgcnVsZXMuZ3RlKT8gJ211c3QgYmUgZ3JlYXRlciB0aGFuIG9yIGVxdWFsIHRvICVzIG9yIGxlc3MgdGhhbiBvciBlcXVhbCB0byAlcycuZm9ybWF0KFtydWxlcy5ndGUsIHJ1bGVzLmx0ZV0pIDogJydIARJ7CgJpbhgGIAMoB0JvwkhsCmoKCmZpeGVkMzIuaW4aXCEodGhpcyBpbiBnZXRGaWVsZChydWxlcywgJ2luJykpID8gJ211c3QgYmUgaW4gbGlzdCAlcycuZm9ybWF0KFtnZXRGaWVsZChydWxlcywgJ2luJyldKSA6ICcnEnIKBm5vdF9pbhgHIAMoB0JiwkhfCl0KDmZpeGVkMzIubm90X2luGkt0aGlzIGluIHJ1bGVzLm5vdF9pbiA/ICdtdXN0IG5vdCBiZSBpbiBsaXN0ICVzJy5mb3JtYXQoW3J1bGVzLm5vdF9pbl0pIDogJycSLQoHZXhhbXBsZRgIIAMoB0IcwkgZChcKD2ZpeGVkMzIuZXhhbXBsZRoEdHJ1ZSoJCOgHEICAgIACQgsKCWxlc3NfdGhhbkIOCgxncmVhdGVyX3RoYW4i0xQKDEZpeGVkNjRSdWxlcxJ/CgVjb25zdBgBIAEoBkJwwkhtCmsKDWZpeGVkNjQuY29uc3QaWnRoaXMgIT0gZ2V0RmllbGQocnVsZXMsICdjb25zdCcpID8gJ211c3QgZXF1YWwgJXMnLmZvcm1hdChbZ2V0RmllbGQocnVsZXMsICdjb25zdCcpXSkgOiAnJxKGAQoCbHQYAiABKAZCeMJIdQpzCgpmaXhlZDY0Lmx0GmUhaGFzKHJ1bGVzLmd0ZSkgJiYgIWhhcyhydWxlcy5ndCkgJiYgdGhpcyA+PSBydWxlcy5sdD8gJ211c3QgYmUgbGVzcyB0aGFuICVzJy5mb3JtYXQoW3J1bGVzLmx0XSkgOiAnJ0gAEpgBCgNsdGUYAyABKAZCiAHCSIQBCoEBCgtmaXhlZDY0Lmx0ZRpyIWhhcyhydWxlcy5ndGUpICYmICFoYXMocnVsZXMuZ3QpICYmIHRoaXMgPiBydWxlcy5sdGU/ICdtdXN0IGJlIGxlc3MgdGhhbiBvciBlcXVhbCB0byAlcycuZm9ybWF0KFtydWxlcy5sdGVdKSA6ICcnSAASgwcKAmd0GAQgASgGQvQGwkjwBgp2CgpmaXhlZDY0Lmd0GmghaGFzKHJ1bGVzLmx0KSAmJiAhaGFzKHJ1bGVzLmx0ZSkgJiYgdGhpcyA8PSBydWxlcy5ndD8gJ211c3QgYmUgZ3JlYXRlciB0aGFuICVzJy5mb3JtYXQoW3J1bGVzLmd0XSkgOiAnJwqvAQoNZml4ZWQ2NC5ndF9sdBqdAWhhcyhydWxlcy5sdCkgJiYgcnVsZXMubHQgPj0gcnVsZXMuZ3QgJiYgKHRoaXMgPj0gcnVsZXMubHQgfHwgdGhpcyA8PSBydWxlcy5ndCk/ICdtdXN0IGJlIGdyZWF0ZXIgdGhhbiAlcyBhbmQgbGVzcyB0aGFuICVzJy5mb3JtYXQoW3J1bGVzLmd0LCBydWxlcy5sdF0pIDogJycKtwEKF2ZpeGVkNjQuZ3RfbHRfZXhjbHVzaXZlGpsBaGFzKHJ1bGVzLmx0KSAmJiBydWxlcy5sdCA8IHJ1bGVzLmd0ICYmIChydWxlcy5sdCA8PSB0aGlzICYmIHRoaXMgPD0gcnVsZXMuZ3QpPyAnbXVzdCBiZSBncmVhdGVyIHRoYW4gJXMgb3IgbGVzcyB0aGFuICVzJy5mb3JtYXQoW3J1bGVzLmd0LCBydWxlcy5sdF0pIDogJycKvwEKDmZpeGVkNjQuZ3RfbHRlGqwBaGFzKHJ1bGVzLmx0ZSkgJiYgcnVsZXMubHRlID49IHJ1bGVzLmd0ICYmICh0aGlzID4gcnVsZXMubHRlIHx8IHRoaXMgPD0gcnVsZXMuZ3QpPyAnbXVzdCBiZSBncmVhdGVyIHRoYW4gJXMgYW5kIGxlc3MgdGhhbiBvciBlcXVhbCB0byAlcycuZm9ybWF0KFtydWxlcy5ndCwgcnVsZXMubHRlXSkgOiAnJwrHAQoYZml4ZWQ2NC5ndF9sdGVfZXhjbHVzaXZlGqoBaGFzKHJ1bGVzLmx0ZSkgJiYgcnVsZXMubHRlIDwgcnVsZXMuZ3QgJiYgKHJ1bGVzLmx0ZSA8IHRoaXMgJiYgdGhpcyA8PSBydWxlcy5ndCk/ICdtdXN0IGJlIGdyZWF0ZXIgdGhhbiAlcyBvciBsZXNzIHRoYW4gb3IgZXF1YWwgdG8gJXMnLmZvcm1hdChbcnVsZXMuZ3QsIHJ1bGVzLmx0ZV0pIDogJydIARLPBwoDZ3RlGAUgASgGQr8Hwki7BwqEAQoLZml4ZWQ2NC5ndGUadSFoYXMocnVsZXMubHQpICYmICFoYXMocnVsZXMubHRlKSAmJiB0aGlzIDwgcnVsZXMuZ3RlPyAnbXVzdCBiZSBncmVhdGVyIHRoYW4gb3IgZXF1YWwgdG8gJXMnLmZvcm1hdChbcnVsZXMuZ3RlXSkgOiAnJwq+AQoOZml4ZWQ2NC5ndGVfbHQaqwFoYXMocnVsZXMubHQpICYmIHJ1bGVzLmx0ID49IHJ1bGVzLmd0ZSAmJiAodGhpcyA+PSBydWxlcy5sdCB8fCB0aGlzIDwgcnVsZXMuZ3RlKT8gJ211c3QgYmUgZ3JlYXRlciB0aGFuIG9yIGVxdWFsIHRvICVzIGFuZCBsZXNzIHRoYW4gJXMnLmZvcm1hdChbcnVsZXMuZ3RlLCBydWxlcy5sdF0pIDogJycKxgEKGGZpeGVkNjQuZ3RlX2x0X2V4Y2x1c2l2ZRqpAWhhcyhydWxlcy5sdCkgJiYgcnVsZXMubHQgPCBydWxlcy5ndGUgJiYgKHJ1bGVzLmx0IDw9IHRoaXMgJiYgdGhpcyA8IHJ1bGVzLmd0ZSk/ICdtdXN0IGJlIGdyZWF0ZXIgdGhhbiBvciBlcXVhbCB0byAlcyBvciBsZXNzIHRoYW4gJXMnLmZvcm1hdChbcnVsZXMuZ3RlLCBydWxlcy5sdF0pIDogJycKzgEKD2ZpeGVkNjQuZ3RlX2x0ZRq6AWhhcyhydWxlcy5sdGUpICYmIHJ1bGVzLmx0ZSA+PSBydWxlcy5ndGUgJiYgKHRoaXMgPiBydWxlcy5sdGUgfHwgdGhpcyA8IHJ1bGVzLmd0ZSk/ICdtdXN0IGJlIGdyZWF0ZXIgdGhhbiBvciBlcXVhbCB0byAlcyBhbmQgbGVzcyB0aGFuIG9yIGVxdWFsIHRvICVzJy5mb3JtYXQoW3J1bGVzLmd0ZSwgcnVsZXMubHRlXSkgOiAnJwrWAQoZZml4ZWQ2NC5ndGVfbHRlX2V4Y2x1c2l2ZRq4AWhhcyhydWxlcy5sdGUpICYmIHJ1bGVzLmx0ZSA8IHJ1bGVzLmd0ZSAmJiAocnVsZXMubHRlIDwgdGhpcyAmJiB0aGlzIDwgcnVsZXMuZ3RlKT8gJ211c3QgYmUgZ3JlYXRlciB0aGFuIG9yIGVxdWFsIHRvICVzIG9yIGxlc3MgdGhhbiBvciBlcXVhbCB0byAlcycuZm9ybWF0KFtydWxlcy5ndGUsIHJ1bGVzLmx0ZV0pIDogJydIARJ7CgJpbhgGIAMoBkJvwkhsCmoKCmZpeGVkNjQuaW4aXCEodGhpcyBpbiBnZXRGaWVsZChydWxlcywgJ2luJykpID8gJ211c3QgYmUgaW4gbGlzdCAlcycuZm9ybWF0KFtnZXRGaWVsZChydWxlcywgJ2luJyldKSA6ICcnEnIKBm5vdF9pbhgHIAMoBkJiwkhfCl0KDmZpeGVkNjQubm90X2luGkt0aGlzIGluIHJ1bGVzLm5vdF9pbiA/ICdtdXN0IG5vdCBiZSBpbiBsaXN0ICVzJy5mb3JtYXQoW3J1bGVzLm5vdF9pbl0pIDogJycSLQoHZXhhbXBsZRgIIAMoBkIcwkgZChcKD2ZpeGVkNjQuZXhhbXBsZRoEdHJ1ZSoJCOgHEICAgIACQgsKCWxlc3NfdGhhbkIOCgxncmVhdGVyX3RoYW4i5RQKDVNGaXhlZDMyUnVsZXMSgAEKBWNvbnN0GAEgASgPQnHCSG4KbAoOc2ZpeGVkMzIuY29uc3QaWnRoaXMgIT0gZ2V0RmllbGQocnVsZXMsICdjb25zdCcpID8gJ211c3QgZXF1YWwgJXMnLmZvcm1hdChbZ2V0RmllbGQocnVsZXMsICdjb25zdCcpXSkgOiAnJxKHAQoCbHQYAiABKA9CecJIdgp0CgtzZml4ZWQzMi5sdBplIWhhcyhydWxlcy5ndGUpICYmICFoYXMocnVsZXMuZ3QpICYmIHRoaXMgPj0gcnVsZXMubHQ/ICdtdXN0IGJlIGxlc3MgdGhhbiAlcycuZm9ybWF0KFtydWxlcy5sdF0pIDogJydIABKZAQoDbHRlGAMgASgPQokBwkiFAQqCAQoMc2ZpeGVkMzIubHRlGnIhaGFzKHJ1bGVzLmd0ZSkgJiYgIWhhcyhydWxlcy5ndCkgJiYgdGhpcyA+IHJ1bGVzLmx0ZT8gJ211c3QgYmUgbGVzcyB0aGFuIG9yIGVxdWFsIHRvICVzJy5mb3JtYXQoW3J1bGVzLmx0ZV0pIDogJydIABKIBwoCZ3QYBCABKA9C+QbCSPUGCncKC3NmaXhlZDMyLmd0GmghaGFzKHJ1bGVzLmx0KSAmJiAhaGFzKHJ1bGVzLmx0ZSkgJiYgdGhpcyA8PSBydWxlcy5ndD8gJ211c3QgYmUgZ3JlYXRlciB0aGFuICVzJy5mb3JtYXQoW3J1bGVzLmd0XSkgOiAnJwqwAQoOc2ZpeGVkMzIuZ3RfbHQanQFoYXMocnVsZXMubHQpICYmIHJ1bGVzLmx0ID49IHJ1bGVzLmd0ICYmICh0aGlzID49IHJ1bGVzLmx0IHx8IHRoaXMgPD0gcnVsZXMuZ3QpPyAnbXVzdCBiZSBncmVhdGVyIHRoYW4gJXMgYW5kIGxlc3MgdGhhbiAlcycuZm9ybWF0KFtydWxlcy5ndCwgcnVsZXMubHRdKSA6ICcnCrgBChhzZml4ZWQzMi5ndF9sdF9leGNsdXNpdmUamwFoYXMocnVsZXMubHQpICYmIHJ1bGVzLmx0IDwgcnVsZXMuZ3QgJiYgKHJ1bGVzLmx0IDw9IHRoaXMgJiYgdGhpcyA8PSBydWxlcy5ndCk/ICdtdXN0IGJlIGdyZWF0ZXIgdGhhbiAlcyBvciBsZXNzIHRoYW4gJXMnLmZvcm1hdChbcnVsZXMuZ3QsIHJ1bGVzLmx0XSkgOiAnJwrAAQoPc2ZpeGVkMzIuZ3RfbHRlGqwBaGFzKHJ1bGVzLmx0ZSkgJiYgcnVsZXMubHRlID49IHJ1bGVzLmd0ICYmICh0aGlzID4gcnVsZXMubHRlIHx8IHRoaXMgPD0gcnVsZXMuZ3QpPyAnbXVzdCBiZSBncmVhdGVyIHRoYW4gJXMgYW5kIGxlc3MgdGhhbiBvciBlcXVhbCB0byAlcycuZm9ybWF0KFtydWxlcy5ndCwgcnVsZXMubHRlXSkgOiAnJwrIAQoZc2ZpeGVkMzIuZ3RfbHRlX2V4Y2x1c2l2ZRqqAWhhcyhydWxlcy5sdGUpICYmIHJ1bGVzLmx0ZSA8IHJ1bGVzLmd0ICYmIChydWxlcy5sdGUgPCB0aGlzICYmIHRoaXMgPD0gcnVsZXMuZ3QpPyAnbXVzdCBiZSBncmVhdGVyIHRoYW4gJXMgb3IgbGVzcyB0aGFuIG9yIGVxdWFsIHRvICVzJy5mb3JtYXQoW3J1bGVzLmd0LCBydWxlcy5sdGVdKSA6ICcnSAES1AcKA2d0ZRgFIAEoD0LEB8JIwAcKhQEKDHNmaXhlZDMyLmd0ZRp1IWhhcyhydWxlcy5sdCkgJiYgIWhhcyhydWxlcy5sdGUpICYmIHRoaXMgPCBydWxlcy5ndGU/ICdtdXN0IGJlIGdyZWF0ZXIgdGhhbiBvciBlcXVhbCB0byAlcycuZm9ybWF0KFtydWxlcy5ndGVdKSA6ICcnCr8BCg9zZml4ZWQzMi5ndGVfbHQaqwFoYXMocnVsZXMubHQpICYmIHJ1bGVzLmx0ID49IHJ1bGVzLmd0ZSAmJiAodGhpcyA+PSBydWxlcy5sdCB8fCB0aGlzIDwgcnVsZXMuZ3RlKT8gJ211c3QgYmUgZ3JlYXRlciB0aGFuIG9yIGVxdWFsIHRvICVzIGFuZCBsZXNzIHRoYW4gJXMnLmZvcm1hdChbcnVsZXMuZ3RlLCBydWxlcy5sdF0pIDogJycKxwEKGXNmaXhlZDMyLmd0ZV9sdF9leGNsdXNpdmUaqQFoYXMocnVsZXMubHQpICYmIHJ1bGVzLmx0IDwgcnVsZXMuZ3RlICYmIChydWxlcy5sdCA8PSB0aGlzICYmIHRoaXMgPCBydWxlcy5ndGUpPyAnbXVzdCBiZSBncmVhdGVyIHRoYW4gb3IgZXF1YWwgdG8gJXMgb3IgbGVzcyB0aGFuICVzJy5mb3JtYXQoW3J1bGVzLmd0ZSwgcnVsZXMubHRdKSA6ICcnCs8BChBzZml4ZWQzMi5ndGVfbHRlGroBaGFzKHJ1bGVzLmx0ZSkgJiYgcnVsZXMubHRlID49IHJ1bGVzLmd0ZSAmJiAodGhpcyA+IHJ1bGVzLmx0ZSB8fCB0aGlzIDwgcnVsZXMuZ3RlKT8gJ211c3QgYmUgZ3JlYXRlciB0aGFuIG9yIGVxdWFsIHRvICVzIGFuZCBsZXNzIHRoYW4gb3IgZXF1YWwgdG8gJXMnLmZvcm1hdChbcnVsZXMuZ3RlLCBydWxlcy5sdGVdKSA6ICcnCtcBChpzZml4ZWQzMi5ndGVfbHRlX2V4Y2x1c2l2ZRq4AWhhcyhydWxlcy5sdGUpICYmIHJ1bGVzLmx0ZSA8IHJ1bGVzLmd0ZSAmJiAocnVsZXMubHRlIDwgdGhpcyAmJiB0aGlzIDwgcnVsZXMuZ3RlKT8gJ211c3QgYmUgZ3JlYXRlciB0aGFuIG9yIGVxdWFsIHRvICVzIG9yIGxlc3MgdGhhbiBvciBlcXVhbCB0byAlcycuZm9ybWF0KFtydWxlcy5ndGUsIHJ1bGVzLmx0ZV0pIDogJydIARJ8CgJpbhgGIAMoD0JwwkhtCmsKC3NmaXhlZDMyLmluGlwhKHRoaXMgaW4gZ2V0RmllbGQocnVsZXMsICdpbicpKSA/ICdtdXN0IGJlIGluIGxpc3QgJXMnLmZvcm1hdChbZ2V0RmllbGQocnVsZXMsICdpbicpXSkgOiAnJxJzCgZub3RfaW4YByADKA9CY8JIYApeCg9zZml4ZWQzMi5ub3RfaW4aS3RoaXMgaW4gcnVsZXMubm90X2luID8gJ211c3Qgbm90IGJlIGluIGxpc3QgJXMnLmZvcm1hdChbcnVsZXMubm90X2luXSkgOiAnJxIuCgdleGFtcGxlGAggAygPQh3CSBoKGAoQc2ZpeGVkMzIuZXhhbXBsZRoEdHJ1ZSoJCOgHEICAgIACQgsKCWxlc3NfdGhhbkIOCgxncmVhdGVyX3RoYW4i5RQKDVNGaXhlZDY0UnVsZXMSgAEKBWNvbnN0GAEgASgQQnHCSG4KbAoOc2ZpeGVkNjQuY29uc3QaWnRoaXMgIT0gZ2V0RmllbGQocnVsZXMsICdjb25zdCcpID8gJ211c3QgZXF1YWwgJXMnLmZvcm1hdChbZ2V0RmllbGQocnVsZXMsICdjb25zdCcpXSkgOiAnJxKHAQoCbHQYAiABKBBCecJIdgp0CgtzZml4ZWQ2NC5sdBplIWhhcyhydWxlcy5ndGUpICYmICFoYXMocnVsZXMuZ3QpICYmIHRoaXMgPj0gcnVsZXMubHQ/ICdtdXN0IGJlIGxlc3MgdGhhbiAlcycuZm9ybWF0KFtydWxlcy5sdF0pIDogJydIABKZAQoDbHRlGAMgASgQQokBwkiFAQqCAQoMc2ZpeGVkNjQubHRlGnIhaGFzKHJ1bGVzLmd0ZSkgJiYgIWhhcyhydWxlcy5ndCkgJiYgdGhpcyA+IHJ1bGVzLmx0ZT8gJ211c3QgYmUgbGVzcyB0aGFuIG9yIGVxdWFsIHRvICVzJy5mb3JtYXQoW3J1bGVzLmx0ZV0pIDogJydIABKIBwoCZ3QYBCABKBBC+QbCSPUGCncKC3NmaXhlZDY0Lmd0GmghaGFzKHJ1bGVzLmx0KSAmJiAhaGFzKHJ1bGVzLmx0ZSkgJiYgdGhpcyA8PSBydWxlcy5ndD8gJ211c3QgYmUgZ3JlYXRlciB0aGFuICVzJy5mb3JtYXQoW3J1bGVzLmd0XSkgOiAnJwqwAQoOc2ZpeGVkNjQuZ3RfbHQanQFoYXMocnVsZXMubHQpICYmIHJ1bGVzLmx0ID49IHJ1bGVzLmd0ICYmICh0aGlzID49IHJ1bGVzLmx0IHx8IHRoaXMgPD0gcnVsZXMuZ3QpPyAnbXVzdCBiZSBncmVhdGVyIHRoYW4gJXMgYW5kIGxlc3MgdGhhbiAlcycuZm9ybWF0KFtydWxlcy5ndCwgcnVsZXMubHRdKSA6ICcnCrgBChhzZml4ZWQ2NC5ndF9sdF9leGNsdXNpdmUamwFoYXMocnVsZXMubHQpICYmIHJ1bGVzLmx0IDwgcnVsZXMuZ3QgJiYgKHJ1bGVzLmx0IDw9IHRoaXMgJiYgdGhpcyA8PSBydWxlcy5ndCk/ICdtdXN0IGJlIGdyZWF0ZXIgdGhhbiAlcyBvciBsZXNzIHRoYW4gJXMnLmZvcm1hdChbcnVsZXMuZ3QsIHJ1bGVzLmx0XSkgOiAnJwrAAQoPc2ZpeGVkNjQuZ3RfbHRlGqwBaGFzKHJ1bGVzLmx0ZSkgJiYgcnVsZXMubHRlID49IHJ1bGVzLmd0ICYmICh0aGlzID4gcnVsZXMubHRlIHx8IHRoaXMgPD0gcnVsZXMuZ3QpPyAnbXVzdCBiZSBncmVhdGVyIHRoYW4gJXMgYW5kIGxlc3MgdGhhbiBvciBlcXVhbCB0byAlcycuZm9ybWF0KFtydWxlcy5ndCwgcnVsZXMubHRlXSkgOiAnJwrIAQoZc2ZpeGVkNjQuZ3RfbHRlX2V4Y2x1c2l2ZRqqAWhhcyhydWxlcy5sdGUpICYmIHJ1bGVzLmx0ZSA8IHJ1bGVzLmd0ICYmIChydWxlcy5sdGUgPCB0aGlzICYmIHRoaXMgPD0gcnVsZXMuZ3QpPyAnbXVzdCBiZSBncmVhdGVyIHRoYW4gJXMgb3IgbGVzcyB0aGFuIG9yIGVxdWFsIHRvICVzJy5mb3JtYXQoW3J1bGVzLmd0LCBydWxlcy5sdGVdKSA6ICcnSAES1AcKA2d0ZRgFIAEoEELEB8JIwAcKhQEKDHNmaXhlZDY0Lmd0ZRp1IWhhcyhydWxlcy5sdCkgJiYgIWhhcyhydWxlcy5sdGUpICYmIHRoaXMgPCBydWxlcy5ndGU/ICdtdXN0IGJlIGdyZWF0ZXIgdGhhbiBvciBlcXVhbCB0byAlcycuZm9ybWF0KFtydWxlcy5ndGVdKSA6ICcnCr8BCg9zZml4ZWQ2NC5ndGVfbHQaqwFoYXMocnVsZXMubHQpICYmIHJ1bGVzLmx0ID49IHJ1bGVzLmd0ZSAmJiAodGhpcyA+PSBydWxlcy5sdCB8fCB0aGlzIDwgcnVsZXMuZ3RlKT8gJ211c3QgYmUgZ3JlYXRlciB0aGFuIG9yIGVxdWFsIHRvICVzIGFuZCBsZXNzIHRoYW4gJXMnLmZvcm1hdChbcnVsZXMuZ3RlLCBydWxlcy5sdF0pIDogJycKxwEKGXNmaXhlZDY0Lmd0ZV9sdF9leGNsdXNpdmUaqQFoYXMocnVsZXMubHQpICYmIHJ1bGVzLmx0IDwgcnVsZXMuZ3RlICYmIChydWxlcy5sdCA8PSB0aGlzICYmIHRoaXMgPCBydWxlcy5ndGUpPyAnbXVzdCBiZSBncmVhdGVyIHRoYW4gb3IgZXF1YWwgdG8gJXMgb3IgbGVzcyB0aGFuICVzJy5mb3JtYXQoW3J1bGVzLmd0ZSwgcnVsZXMubHRdKSA6ICcnCs8BChBzZml4ZWQ2NC5ndGVfbHRlGroBaGFzKHJ1bGVzLmx0ZSkgJiYgcnVsZXMubHRlID49IHJ1bGVzLmd0ZSAmJiAodGhpcyA+IHJ1bGVzLmx0ZSB8fCB0aGlzIDwgcnVsZXMuZ3RlKT8gJ211c3QgYmUgZ3JlYXRlciB0aGFuIG9yIGVxdWFsIHRvICVzIGFuZCBsZXNzIHRoYW4gb3IgZXF1YWwgdG8gJXMnLmZvcm1hdChbcnVsZXMuZ3RlLCBydWxlcy5sdGVdKSA6ICcnCtcBChpzZml4ZWQ2NC5ndGVfbHRlX2V4Y2x1c2l2ZRq4AWhhcyhydWxlcy5sdGUpICYmIHJ1bGVzLmx0ZSA8IHJ1bGVzLmd0ZSAmJiAocnVsZXMubHRlIDwgdGhpcyAmJiB0aGlzIDwgcnVsZXMuZ3RlKT8gJ211c3QgYmUgZ3JlYXRlciB0aGFuIG9yIGVxdWFsIHRvICVzIG9yIGxlc3MgdGhhbiBvciBlcXVhbCB0byAlcycuZm9ybWF0KFtydWxlcy5ndGUsIHJ1bGVzLmx0ZV0pIDogJydIARJ8CgJpbhgGIAMoEEJwwkhtCmsKC3NmaXhlZDY0LmluGlwhKHRoaXMgaW4gZ2V0RmllbGQocnVsZXMsICdpbicpKSA/ICdtdXN0IGJlIGluIGxpc3QgJXMnLmZvcm1hdChbZ2V0RmllbGQocnVsZXMsICdpbicpXSkgOiAnJxJzCgZub3RfaW4YByADKBBCY8JIYApeCg9zZml4ZWQ2NC5ub3RfaW4aS3RoaXMgaW4gcnVsZXMubm90X2luID8gJ211c3Qgbm90IGJlIGluIGxpc3QgJXMnLmZvcm1hdChbcnVsZXMubm90X2luXSkgOiAnJxIuCgdleGFtcGxlGAggAygQQh3CSBoKGAoQc2ZpeGVkNjQuZXhhbXBsZRoEdHJ1ZSoJCOgHEICAgIACQgsKCWxlc3NfdGhhbkIOCgxncmVhdGVyX3RoYW4iwAEKCUJvb2xSdWxlcxJ8CgVjb25zdBgBIAEoCEJtwkhqCmgKCmJvb2wuY29uc3QaWnRoaXMgIT0gZ2V0RmllbGQocnVsZXMsICdjb25zdCcpID8gJ211c3QgZXF1YWwgJXMnLmZvcm1hdChbZ2V0RmllbGQocnVsZXMsICdjb25zdCcpXSkgOiAnJxIqCgdleGFtcGxlGAIgAygIQhnCSBYKFAoMYm9vbC5leGFtcGxlGgR0cnVlKgkI6AcQgICAgAIi6jwKC1N0cmluZ1J1bGVzEoABCgVjb25zdBgBIAEoCUJxwkhuCmwKDHN0cmluZy5jb25zdBpcdGhpcyAhPSBnZXRGaWVsZChydWxlcywgJ2NvbnN0JykgPyAnbXVzdCBlcXVhbCBgJXNgJy5mb3JtYXQoW2dldEZpZWxkKHJ1bGVzLCAnY29uc3QnKV0pIDogJycScQoDbGVuGBMgASgEQmTCSGEKXwoKc3RyaW5nLmxlbhpRdWludCh0aGlzLnNpemUoKSkgIT0gcnVsZXMubGVuID8gJ211c3QgYmUgJXMgY2hhcmFjdGVycycuZm9ybWF0KFtydWxlcy5sZW5dKSA6ICcnEokBCgdtaW5fbGVuGAIgASgEQnjCSHUKcwoOc3RyaW5nLm1pbl9sZW4aYXVpbnQodGhpcy5zaXplKCkpIDwgcnVsZXMubWluX2xlbiA/ICdtdXN0IGJlIGF0IGxlYXN0ICVzIGNoYXJhY3RlcnMnLmZvcm1hdChbcnVsZXMubWluX2xlbl0pIDogJycSiAEKB21heF9sZW4YAyABKARCd8JIdApyCg5zdHJpbmcubWF4X2xlbhpgdWludCh0aGlzLnNpemUoKSkgPiBydWxlcy5tYXhfbGVuID8gJ211c3QgYmUgYXQgbW9zdCAlcyBjaGFyYWN0ZXJzJy5mb3JtYXQoW3J1bGVzLm1heF9sZW5dKSA6ICcnEosBCglsZW5fYnl0ZXMYFCABKARCeMJIdQpzChBzdHJpbmcubGVuX2J5dGVzGl91aW50KGJ5dGVzKHRoaXMpLnNpemUoKSkgIT0gcnVsZXMubGVuX2J5dGVzID8gJ211c3QgYmUgJXMgYnl0ZXMnLmZvcm1hdChbcnVsZXMubGVuX2J5dGVzXSkgOiAnJxKUAQoJbWluX2J5dGVzGAQgASgEQoABwkh9CnsKEHN0cmluZy5taW5fYnl0ZXMaZ3VpbnQoYnl0ZXModGhpcykuc2l6ZSgpKSA8IHJ1bGVzLm1pbl9ieXRlcyA/ICdtdXN0IGJlIGF0IGxlYXN0ICVzIGJ5dGVzJy5mb3JtYXQoW3J1bGVzLm1pbl9ieXRlc10pIDogJycSkgEKCW1heF9ieXRlcxgFIAEoBEJ/wkh8CnoKEHN0cmluZy5tYXhfYnl0ZXMaZnVpbnQoYnl0ZXModGhpcykuc2l6ZSgpKSA+IHJ1bGVzLm1heF9ieXRlcyA/ICdtdXN0IGJlIGF0IG1vc3QgJXMgYnl0ZXMnLmZvcm1hdChbcnVsZXMubWF4X2J5dGVzXSkgOiAnJxKHAQoHcGF0dGVybhgGIAEoCUJ2wkhzCnEKDnN0cmluZy5wYXR0ZXJuGl8hdGhpcy5tYXRjaGVzKHJ1bGVzLnBhdHRlcm4pID8gJ2RvZXMgbm90IG1hdGNoIHJlZ2V4IHBhdHRlcm4gYCVzYCcuZm9ybWF0KFtydWxlcy5wYXR0ZXJuXSkgOiAnJxJ+CgZwcmVmaXgYByABKAlCbsJIawppCg1zdHJpbmcucHJlZml4GlghdGhpcy5zdGFydHNXaXRoKHJ1bGVzLnByZWZpeCkgPyAnZG9lcyBub3QgaGF2ZSBwcmVmaXggYCVzYCcuZm9ybWF0KFtydWxlcy5wcmVmaXhdKSA6ICcnEnwKBnN1ZmZpeBgIIAEoCUJswkhpCmcKDXN0cmluZy5zdWZmaXgaViF0aGlzLmVuZHNXaXRoKHJ1bGVzLnN1ZmZpeCkgPyAnZG9lcyBub3QgaGF2ZSBzdWZmaXggYCVzYCcuZm9ybWF0KFtydWxlcy5zdWZmaXhdKSA6ICcnEooBCghjb250YWlucxgJIAEoCUJ4wkh1CnMKD3N0cmluZy5jb250YWlucxpgIXRoaXMuY29udGFpbnMocnVsZXMuY29udGFpbnMpID8gJ2RvZXMgbm90IGNvbnRhaW4gc3Vic3RyaW5nIGAlc2AnLmZvcm1hdChbcnVsZXMuY29udGFpbnNdKSA6ICcnEpEBCgxub3RfY29udGFpbnMYFyABKAlCe8JIeAp2ChNzdHJpbmcubm90X2NvbnRhaW5zGl90aGlzLmNvbnRhaW5zKHJ1bGVzLm5vdF9jb250YWlucykgPyAnY29udGFpbnMgc3Vic3RyaW5nIGAlc2AnLmZvcm1hdChbcnVsZXMubm90X2NvbnRhaW5zXSkgOiAnJxJ6CgJpbhgKIAMoCUJuwkhrCmkKCXN0cmluZy5pbhpcISh0aGlzIGluIGdldEZpZWxkKHJ1bGVzLCAnaW4nKSkgPyAnbXVzdCBiZSBpbiBsaXN0ICVzJy5mb3JtYXQoW2dldEZpZWxkKHJ1bGVzLCAnaW4nKV0pIDogJycScQoGbm90X2luGAsgAygJQmHCSF4KXAoNc3RyaW5nLm5vdF9pbhpLdGhpcyBpbiBydWxlcy5ub3RfaW4gPyAnbXVzdCBub3QgYmUgaW4gbGlzdCAlcycuZm9ybWF0KFtydWxlcy5ub3RfaW5dKSA6ICcnEtkBCgVlbWFpbBgMIAEoCELHAcJIwwEKWwoMc3RyaW5nLmVtYWlsEh1tdXN0IGJlIGEgdmFsaWQgZW1haWwgYWRkcmVzcxosIXJ1bGVzLmVtYWlsIHx8IHRoaXMgPT0gJycgfHwgdGhpcy5pc0VtYWlsKCkKZAoSc3RyaW5nLmVtYWlsX2VtcHR5EjJ2YWx1ZSBpcyBlbXB0eSwgd2hpY2ggaXMgbm90IGEgdmFsaWQgZW1haWwgYWRkcmVzcxoaIXJ1bGVzLmVtYWlsIHx8IHRoaXMgIT0gJydIABLhAQoIaG9zdG5hbWUYDSABKAhCzAHCSMgBCl8KD3N0cmluZy5ob3N0bmFtZRIYbXVzdCBiZSBhIHZhbGlkIGhvc3RuYW1lGjIhcnVsZXMuaG9zdG5hbWUgfHwgdGhpcyA9PSAnJyB8fCB0aGlzLmlzSG9zdG5hbWUoKQplChVzdHJpbmcuaG9zdG5hbWVfZW1wdHkSLXZhbHVlIGlzIGVtcHR5LCB3aGljaCBpcyBub3QgYSB2YWxpZCBob3N0bmFtZRodIXJ1bGVzLmhvc3RuYW1lIHx8IHRoaXMgIT0gJydIABLBAQoCaXAYDiABKAhCsgHCSK4BCk8KCXN0cmluZy5pcBIabXVzdCBiZSBhIHZhbGlkIElQIGFkZHJlc3MaJiFydWxlcy5pcCB8fCB0aGlzID09ICcnIHx8IHRoaXMuaXNJcCgpClsKD3N0cmluZy5pcF9lbXB0eRIvdmFsdWUgaXMgZW1wdHksIHdoaWNoIGlzIG5vdCBhIHZhbGlkIElQIGFkZHJlc3MaFyFydWxlcy5pcCB8fCB0aGlzICE9ICcnSAAS0AEKBGlwdjQYDyABKAhCvwHCSLsBClYKC3N0cmluZy5pcHY0EhxtdXN0IGJlIGEgdmFsaWQgSVB2NCBhZGRyZXNzGikhcnVsZXMuaXB2NCB8fCB0aGlzID09ICcnIHx8IHRoaXMuaXNJcCg0KQphChFzdHJpbmcuaXB2NF9lbXB0eRIxdmFsdWUgaXMgZW1wdHksIHdoaWNoIGlzIG5vdCBhIHZhbGlkIElQdjQgYWRkcmVzcxoZIXJ1bGVzLmlwdjQgfHwgdGhpcyAhPSAnJ0gAEtABCgRpcHY2GBAgASgIQr8Bwki7AQpWCgtzdHJpbmcuaXB2NhIcbXVzdCBiZSBhIHZhbGlkIElQdjYgYWRkcmVzcxopIXJ1bGVzLmlwdjYgfHwgdGhpcyA9PSAnJyB8fCB0aGlzLmlzSXAoNikKYQoRc3RyaW5nLmlwdjZfZW1wdHkSMXZhbHVlIGlzIGVtcHR5LCB3aGljaCBpcyBub3QgYSB2YWxpZCBJUHY2IGFkZHJlc3MaGSFydWxlcy5pcHY2IHx8IHRoaXMgIT0gJydIABK5AQoDdXJpGBEgASgIQqkBwkilAQpLCgpzdHJpbmcudXJpEhNtdXN0IGJlIGEgdmFsaWQgVVJJGighcnVsZXMudXJpIHx8IHRoaXMgPT0gJycgfHwgdGhpcy5pc1VyaSgpClYKEHN0cmluZy51cmlfZW1wdHkSKHZhbHVlIGlzIGVtcHR5LCB3aGljaCBpcyBub3QgYSB2YWxpZCBVUkkaGCFydWxlcy51cmkgfHwgdGhpcyAhPSAnJ0gAEmoKB3VyaV9yZWYYEiABKAhCV8JIVApSCg5zdHJpbmcudXJpX3JlZhIdbXVzdCBiZSBhIHZhbGlkIFVSSSBSZWZlcmVuY2UaISFydWxlcy51cmlfcmVmIHx8IHRoaXMuaXNVcmlSZWYoKUgAEokCCgdhZGRyZXNzGBUgASgIQvUBwkjxAQp7Cg5zdHJpbmcuYWRkcmVzcxInbXVzdCBiZSBhIHZhbGlkIGhvc3RuYW1lLCBvciBpcCBhZGRyZXNzGkAhcnVsZXMuYWRkcmVzcyB8fCB0aGlzID09ICcnIHx8IHRoaXMuaXNIb3N0bmFtZSgpIHx8IHRoaXMuaXNJcCgpCnIKFHN0cmluZy5hZGRyZXNzX2VtcHR5Ejx2YWx1ZSBpcyBlbXB0eSwgd2hpY2ggaXMgbm90IGEgdmFsaWQgaG9zdG5hbWUsIG9yIGlwIGFkZHJlc3MaHCFydWxlcy5hZGRyZXNzIHx8IHRoaXMgIT0gJydIABKSAgoEdXVpZBgWIAEoCEKBAsJI/QEKnwEKC3N0cmluZy51dWlkEhRtdXN0IGJlIGEgdmFsaWQgVVVJRBp6IXJ1bGVzLnV1aWQgfHwgdGhpcyA9PSAnJyB8fCB0aGlzLm1hdGNoZXMoJ15bMC05YS1mQS1GXXs4fS1bMC05YS1mQS1GXXs0fS1bMC05YS1mQS1GXXs0fS1bMC05YS1mQS1GXXs0fS1bMC05YS1mQS1GXXsxMn0kJykKWQoRc3RyaW5nLnV1aWRfZW1wdHkSKXZhbHVlIGlzIGVtcHR5LCB3aGljaCBpcyBub3QgYSB2YWxpZCBVVUlEGhkhcnVsZXMudXVpZCB8fCB0aGlzICE9ICcnSAAS6gEKBXR1dWlkGCEgASgIQtgBwkjUAQptCgxzdHJpbmcudHV1aWQSHG11c3QgYmUgYSB2YWxpZCB0cmltbWVkIFVVSUQaPyFydWxlcy50dXVpZCB8fCB0aGlzID09ICcnIHx8IHRoaXMubWF0Y2hlcygnXlswLTlhLWZBLUZdezMyfSQnKQpjChJzdHJpbmcudHV1aWRfZW1wdHkSMXZhbHVlIGlzIGVtcHR5LCB3aGljaCBpcyBub3QgYSB2YWxpZCB0cmltbWVkIFVVSUQaGiFydWxlcy50dXVpZCB8fCB0aGlzICE9ICcnSAASkAIKEWlwX3dpdGhfcHJlZml4bGVuGBogASgIQvIBwkjuAQpyChhzdHJpbmcuaXBfd2l0aF9wcmVmaXhsZW4SGW11c3QgYmUgYSB2YWxpZCBJUCBwcmVmaXgaOyFydWxlcy5pcF93aXRoX3ByZWZpeGxlbiB8fCB0aGlzID09ICcnIHx8IHRoaXMuaXNJcFByZWZpeCgpCngKHnN0cmluZy5pcF93aXRoX3ByZWZpeGxlbl9lbXB0eRIudmFsdWUgaXMgZW1wdHksIHdoaWNoIGlzIG5vdCBhIHZhbGlkIElQIHByZWZpeBomIXJ1bGVzLmlwX3dpdGhfcHJlZml4bGVuIHx8IHRoaXMgIT0gJydIABLJAgoTaXB2NF93aXRoX3ByZWZpeGxlbhgbIAEoCEKpAsJIpQIKjQEKGnN0cmluZy5pcHY0X3dpdGhfcHJlZml4bGVuEi9tdXN0IGJlIGEgdmFsaWQgSVB2NCBhZGRyZXNzIHdpdGggcHJlZml4IGxlbmd0aBo+IXJ1bGVzLmlwdjRfd2l0aF9wcmVmaXhsZW4gfHwgdGhpcyA9PSAnJyB8fCB0aGlzLmlzSXBQcmVmaXgoNCkKkgEKIHN0cmluZy5pcHY0X3dpdGhfcHJlZml4bGVuX2VtcHR5EkR2YWx1ZSBpcyBlbXB0eSwgd2hpY2ggaXMgbm90IGEgdmFsaWQgSVB2NCBhZGRyZXNzIHdpdGggcHJlZml4IGxlbmd0aBooIXJ1bGVzLmlwdjRfd2l0aF9wcmVmaXhsZW4gfHwgdGhpcyAhPSAnJ0gAEskCChNpcHY2X3dpdGhfcHJlZml4bGVuGBwgASgIQqkCwkilAgqNAQoac3RyaW5nLmlwdjZfd2l0aF9wcmVmaXhsZW4SL211c3QgYmUgYSB2YWxpZCBJUHY2IGFkZHJlc3Mgd2l0aCBwcmVmaXggbGVuZ3RoGj4hcnVsZXMuaXB2Nl93aXRoX3ByZWZpeGxlbiB8fCB0aGlzID09ICcnIHx8IHRoaXMuaXNJcFByZWZpeCg2KQqSAQogc3RyaW5nLmlwdjZfd2l0aF9wcmVmaXhsZW5fZW1wdHkSRHZhbHVlIGlzIGVtcHR5LCB3aGljaCBpcyBub3QgYSB2YWxpZCBJUHY2IGFkZHJlc3Mgd2l0aCBwcmVmaXggbGVuZ3RoGighcnVsZXMuaXB2Nl93aXRoX3ByZWZpeGxlbiB8fCB0aGlzICE9ICcnSAAS7AEKCWlwX3ByZWZpeBgdIAEoCELWAcJI0gEKZgoQc3RyaW5nLmlwX3ByZWZpeBIZbXVzdCBiZSBhIHZhbGlkIElQIHByZWZpeBo3IXJ1bGVzLmlwX3ByZWZpeCB8fCB0aGlzID09ICcnIHx8IHRoaXMuaXNJcFByZWZpeCh0cnVlKQpoChZzdHJpbmcuaXBfcHJlZml4X2VtcHR5Ei52YWx1ZSBpcyBlbXB0eSwgd2hpY2ggaXMgbm90IGEgdmFsaWQgSVAgcHJlZml4Gh4hcnVsZXMuaXBfcHJlZml4IHx8IHRoaXMgIT0gJydIABL9AQoLaXB2NF9wcmVmaXgYHiABKAhC5QHCSOEBCm8KEnN0cmluZy5pcHY0X3ByZWZpeBIbbXVzdCBiZSBhIHZhbGlkIElQdjQgcHJlZml4GjwhcnVsZXMuaXB2NF9wcmVmaXggfHwgdGhpcyA9PSAnJyB8fCB0aGlzLmlzSXBQcmVmaXgoNCwgdHJ1ZSkKbgoYc3RyaW5nLmlwdjRfcHJlZml4X2VtcHR5EjB2YWx1ZSBpcyBlbXB0eSwgd2hpY2ggaXMgbm90IGEgdmFsaWQgSVB2NCBwcmVmaXgaICFydWxlcy5pcHY0X3ByZWZpeCB8fCB0aGlzICE9ICcnSAAS/QEKC2lwdjZfcHJlZml4GB8gASgIQuUBwkjhAQpvChJzdHJpbmcuaXB2Nl9wcmVmaXgSG211c3QgYmUgYSB2YWxpZCBJUHY2IHByZWZpeBo8IXJ1bGVzLmlwdjZfcHJlZml4IHx8IHRoaXMgPT0gJycgfHwgdGhpcy5pc0lwUHJlZml4KDYsIHRydWUpCm4KGHN0cmluZy5pcHY2X3ByZWZpeF9lbXB0eRIwdmFsdWUgaXMgZW1wdHksIHdoaWNoIGlzIG5vdCBhIHZhbGlkIElQdjYgcHJlZml4GiAhcnVsZXMuaXB2Nl9wcmVmaXggfHwgdGhpcyAhPSAnJ0gAEq8CCg1ob3N0X2FuZF9wb3J0GCAgASgIQpUCwkiRAgqTAQoUc3RyaW5nLmhvc3RfYW5kX3BvcnQSO211c3QgYmUgYSB2YWxpZCBob3N0IChob3N0bmFtZSBvciBJUCBhZGRyZXNzKSBhbmQgcG9ydCBwYWlyGj4hcnVsZXMuaG9zdF9hbmRfcG9ydCB8fCB0aGlzID09ICcnIHx8IHRoaXMuaXNIb3N0QW5kUG9ydCh0cnVlKQp5ChpzdHJpbmcuaG9zdF9hbmRfcG9ydF9lbXB0eRI3dmFsdWUgaXMgZW1wdHksIHdoaWNoIGlzIG5vdCBhIHZhbGlkIGhvc3QgYW5kIHBvcnQgcGFpchoiIXJ1bGVzLmhvc3RfYW5kX3BvcnQgfHwgdGhpcyAhPSAnJ0gAEu4BCgR1bGlkGCMgASgIQt0BwkjZAQp8CgtzdHJpbmcudWxpZBIUbXVzdCBiZSBhIHZhbGlkIFVMSUQaVyFydWxlcy51bGlkIHx8IHRoaXMgPT0gJycgfHwgdGhpcy5tYXRjaGVzKCdeWzAtN11bMC05QS1ISktNTlAtVFYtWmEtaGprbW5wLXR2LXpdezI1fSQnKQpZChFzdHJpbmcudWxpZF9lbXB0eRIpdmFsdWUgaXMgZW1wdHksIHdoaWNoIGlzIG5vdCBhIHZhbGlkIFVMSUQaGSFydWxlcy51bGlkIHx8IHRoaXMgIT0gJydIABLUAgoMcHJvdG9idWZfZnFuGCUgASgIQrsCwki3AgqvAQoTc3RyaW5nLnByb3RvYnVmX2ZxbhItbXVzdCBiZSBhIHZhbGlkIGZ1bGx5LXF1YWxpZmllZCBQcm90b2J1ZiBuYW1lGmkhcnVsZXMucHJvdG9idWZfZnFuIHx8IHRoaXMgPT0gJycgfHwgdGhpcy5tYXRjaGVzKCdeW0EtWmEtel9dW0EtWmEtel8wLTldKihcXC5bQS1aYS16X11bQS1aYS16XzAtOV0qKSokJykKggEKGXN0cmluZy5wcm90b2J1Zl9mcW5fZW1wdHkSQnZhbHVlIGlzIGVtcHR5LCB3aGljaCBpcyBub3QgYSB2YWxpZCBmdWxseS1xdWFsaWZpZWQgUHJvdG9idWYgbmFtZRohIXJ1bGVzLnByb3RvYnVmX2ZxbiB8fCB0aGlzICE9ICcnSAASkQMKEHByb3RvYnVmX2RvdF9mcW4YJiABKAhC9ALCSPACCs0BChdzdHJpbmcucHJvdG9idWZfZG90X2ZxbhJAbXVzdCBiZSBhIHZhbGlkIGZ1bGx5LXF1YWxpZmllZCBQcm90b2J1ZiBuYW1lIHdpdGggYSBsZWFkaW5nIGRvdBpwIXJ1bGVzLnByb3RvYnVmX2RvdF9mcW4gfHwgdGhpcyA9PSAnJyB8fCB0aGlzLm1hdGNoZXMoJ15cXC5bQS1aYS16X11bQS1aYS16XzAtOV0qKFxcLltBLVphLXpfXVtBLVphLXpfMC05XSopKiQnKQqdAQodc3RyaW5nLnByb3RvYnVmX2RvdF9mcW5fZW1wdHkSVXZhbHVlIGlzIGVtcHR5LCB3aGljaCBpcyBub3QgYSB2YWxpZCBmdWxseS1xdWFsaWZpZWQgUHJvdG9idWYgbmFtZSB3aXRoIGEgbGVhZGluZyBkb3QaJSFydWxlcy5wcm90b2J1Zl9kb3RfZnFuIHx8IHRoaXMgIT0gJydIABKcBQoQd2VsbF9rbm93bl9yZWdleBgYIAEoDjIYLmJ1Zi52YWxpZGF0ZS5Lbm93blJlZ2V4QuUEwkjhBArqAQojc3RyaW5nLndlbGxfa25vd25fcmVnZXguaGVhZGVyX25hbWUSIG11c3QgYmUgYSB2YWxpZCBIVFRQIGhlYWRlciBuYW1lGqABcnVsZXMud2VsbF9rbm93bl9yZWdleCAhPSAxIHx8IHRoaXMgPT0gJycgfHwgdGhpcy5tYXRjaGVzKCFoYXMocnVsZXMuc3RyaWN0KSB8fCBydWxlcy5zdHJpY3QgPydeOj9bMC05YS16QS1aISMkJSZcJyorLS5eX3x+XHg2MF0rJCcgOideW15cdTAwMDBcdTAwMEFcdTAwMERdKyQnKQqNAQopc3RyaW5nLndlbGxfa25vd25fcmVnZXguaGVhZGVyX25hbWVfZW1wdHkSNXZhbHVlIGlzIGVtcHR5LCB3aGljaCBpcyBub3QgYSB2YWxpZCBIVFRQIGhlYWRlciBuYW1lGilydWxlcy53ZWxsX2tub3duX3JlZ2V4ICE9IDEgfHwgdGhpcyAhPSAnJwrhAQokc3RyaW5nLndlbGxfa25vd25fcmVnZXguaGVhZGVyX3ZhbHVlEiFtdXN0IGJlIGEgdmFsaWQgSFRUUCBoZWFkZXIgdmFsdWUalQFydWxlcy53ZWxsX2tub3duX3JlZ2V4ICE9IDIgfHwgdGhpcy5tYXRjaGVzKCFoYXMocnVsZXMuc3RyaWN0KSB8fCBydWxlcy5zdHJpY3QgPydeW15cdTAwMDAtXHUwMDA4XHUwMDBBLVx1MDAxRlx1MDA3Rl0qJCcgOideW15cdTAwMDBcdTAwMEFcdTAwMERdKiQnKUgAEg4KBnN0cmljdBgZIAEoCBIsCgdleGFtcGxlGCIgAygJQhvCSBgKFgoOc3RyaW5nLmV4YW1wbGUaBHRydWUqCQjoBxCAgICAAkIMCgp3ZWxsX2tub3duIt0RCgpCeXRlc1J1bGVzEnoKBWNvbnN0GAEgASgMQmvCSGgKZgoLYnl0ZXMuY29uc3QaV3RoaXMgIT0gZ2V0RmllbGQocnVsZXMsICdjb25zdCcpID8gJ211c3QgYmUgJXgnLmZvcm1hdChbZ2V0RmllbGQocnVsZXMsICdjb25zdCcpXSkgOiAnJxJrCgNsZW4YDSABKARCXsJIWwpZCglieXRlcy5sZW4aTHVpbnQodGhpcy5zaXplKCkpICE9IHJ1bGVzLmxlbiA/ICdtdXN0IGJlICVzIGJ5dGVzJy5mb3JtYXQoW3J1bGVzLmxlbl0pIDogJycSgwEKB21pbl9sZW4YAiABKARCcsJIbwptCg1ieXRlcy5taW5fbGVuGlx1aW50KHRoaXMuc2l6ZSgpKSA8IHJ1bGVzLm1pbl9sZW4gPyAnbXVzdCBiZSBhdCBsZWFzdCAlcyBieXRlcycuZm9ybWF0KFtydWxlcy5taW5fbGVuXSkgOiAnJxKCAQoHbWF4X2xlbhgDIAEoBEJxwkhuCmwKDWJ5dGVzLm1heF9sZW4aW3VpbnQodGhpcy5zaXplKCkpID4gcnVsZXMubWF4X2xlbiA/ICdtdXN0IGJlIGF0IG1vc3QgJXMgYnl0ZXMnLmZvcm1hdChbcnVsZXMubWF4X2xlbl0pIDogJycSigEKB3BhdHRlcm4YBCABKAlCecJIdgp0Cg1ieXRlcy5wYXR0ZXJuGmMhc3RyaW5nKHRoaXMpLm1hdGNoZXMocnVsZXMucGF0dGVybikgPyAnbXVzdCBtYXRjaCByZWdleCBwYXR0ZXJuIGAlc2AnLmZvcm1hdChbcnVsZXMucGF0dGVybl0pIDogJycSewoGcHJlZml4GAUgASgMQmvCSGgKZgoMYnl0ZXMucHJlZml4GlYhdGhpcy5zdGFydHNXaXRoKHJ1bGVzLnByZWZpeCkgPyAnZG9lcyBub3QgaGF2ZSBwcmVmaXggJXgnLmZvcm1hdChbcnVsZXMucHJlZml4XSkgOiAnJxJ5CgZzdWZmaXgYBiABKAxCacJIZgpkCgxieXRlcy5zdWZmaXgaVCF0aGlzLmVuZHNXaXRoKHJ1bGVzLnN1ZmZpeCkgPyAnZG9lcyBub3QgaGF2ZSBzdWZmaXggJXgnLmZvcm1hdChbcnVsZXMuc3VmZml4XSkgOiAnJxJ9Cghjb250YWlucxgHIAEoDEJrwkhoCmYKDmJ5dGVzLmNvbnRhaW5zGlQhdGhpcy5jb250YWlucyhydWxlcy5jb250YWlucykgPyAnZG9lcyBub3QgY29udGFpbiAleCcuZm9ybWF0KFtydWxlcy5jb250YWluc10pIDogJycSoQEKAmluGAggAygMQpQBwkiQAQqNAQoIYnl0ZXMuaW4agAFnZXRGaWVsZChydWxlcywgJ2luJykuc2l6ZSgpID4gMCAmJiAhKHRoaXMgaW4gZ2V0RmllbGQocnVsZXMsICdpbicpKSA/ICdtdXN0IGJlIGluIGxpc3QgJXMnLmZvcm1hdChbZ2V0RmllbGQocnVsZXMsICdpbicpXSkgOiAnJxJwCgZub3RfaW4YCSADKAxCYMJIXQpbCgxieXRlcy5ub3RfaW4aS3RoaXMgaW4gcnVsZXMubm90X2luID8gJ211c3Qgbm90IGJlIGluIGxpc3QgJXMnLmZvcm1hdChbcnVsZXMubm90X2luXSkgOiAnJxLlAQoCaXAYCiABKAhC1gHCSNIBCm4KCGJ5dGVzLmlwEhptdXN0IGJlIGEgdmFsaWQgSVAgYWRkcmVzcxpGIXJ1bGVzLmlwIHx8IHRoaXMuc2l6ZSgpID09IDAgfHwgdGhpcy5zaXplKCkgPT0gNCB8fCB0aGlzLnNpemUoKSA9PSAxNgpgCg5ieXRlcy5pcF9lbXB0eRIvdmFsdWUgaXMgZW1wdHksIHdoaWNoIGlzIG5vdCBhIHZhbGlkIElQIGFkZHJlc3MaHSFydWxlcy5pcCB8fCB0aGlzLnNpemUoKSAhPSAwSAAS3gEKBGlwdjQYCyABKAhCzQHCSMkBCl8KCmJ5dGVzLmlwdjQSHG11c3QgYmUgYSB2YWxpZCBJUHY0IGFkZHJlc3MaMyFydWxlcy5pcHY0IHx8IHRoaXMuc2l6ZSgpID09IDAgfHwgdGhpcy5zaXplKCkgPT0gNApmChBieXRlcy5pcHY0X2VtcHR5EjF2YWx1ZSBpcyBlbXB0eSwgd2hpY2ggaXMgbm90IGEgdmFsaWQgSVB2NCBhZGRyZXNzGh8hcnVsZXMuaXB2NCB8fCB0aGlzLnNpemUoKSAhPSAwSAAS3wEKBGlwdjYYDCABKAhCzgHCSMoBCmAKCmJ5dGVzLmlwdjYSHG11c3QgYmUgYSB2YWxpZCBJUHY2IGFkZHJlc3MaNCFydWxlcy5pcHY2IHx8IHRoaXMuc2l6ZSgpID09IDAgfHwgdGhpcy5zaXplKCkgPT0gMTYKZgoQYnl0ZXMuaXB2Nl9lbXB0eRIxdmFsdWUgaXMgZW1wdHksIHdoaWNoIGlzIG5vdCBhIHZhbGlkIElQdjYgYWRkcmVzcxofIXJ1bGVzLmlwdjYgfHwgdGhpcy5zaXplKCkgIT0gMEgAEs8BCgR1dWlkGA8gASgIQr4Bwki6AQpYCgpieXRlcy51dWlkEhRtdXN0IGJlIGEgdmFsaWQgVVVJRBo0IXJ1bGVzLnV1aWQgfHwgdGhpcy5zaXplKCkgPT0gMCB8fCB0aGlzLnNpemUoKSA9PSAxNgpeChBieXRlcy51dWlkX2VtcHR5Eil2YWx1ZSBpcyBlbXB0eSwgd2hpY2ggaXMgbm90IGEgdmFsaWQgVVVJRBofIXJ1bGVzLnV1aWQgfHwgdGhpcy5zaXplKCkgIT0gMEgAEisKB2V4YW1wbGUYDiADKAxCGsJIFwoVCg1ieXRlcy5leGFtcGxlGgR0cnVlKgkI6AcQgICAgAJCDAoKd2VsbF9rbm93biLBAwoJRW51bVJ1bGVzEnwKBWNvbnN0GAEgASgFQm3CSGoKaAoKZW51bS5jb25zdBpadGhpcyAhPSBnZXRGaWVsZChydWxlcywgJ2NvbnN0JykgPyAnbXVzdCBlcXVhbCAlcycuZm9ybWF0KFtnZXRGaWVsZChydWxlcywgJ2NvbnN0JyldKSA6ICcnEhQKDGRlZmluZWRfb25seRgCIAEoCBJ4CgJpbhgDIAMoBUJswkhpCmcKB2VudW0uaW4aXCEodGhpcyBpbiBnZXRGaWVsZChydWxlcywgJ2luJykpID8gJ211c3QgYmUgaW4gbGlzdCAlcycuZm9ybWF0KFtnZXRGaWVsZChydWxlcywgJ2luJyldKSA6ICcnEm8KBm5vdF9pbhgEIAMoBUJfwkhcCloKC2VudW0ubm90X2luGkt0aGlzIGluIHJ1bGVzLm5vdF9pbiA/ICdtdXN0IG5vdCBiZSBpbiBsaXN0ICVzJy5mb3JtYXQoW3J1bGVzLm5vdF9pbl0pIDogJycSKgoHZXhhbXBsZRgFIAMoBUIZwkgWChQKDGVudW0uZXhhbXBsZRoEdHJ1ZSoJCOgHEICAgIACIu0DCg1SZXBlYXRlZFJ1bGVzEpYBCgltaW5faXRlbXMYASABKARCggHCSH8KfQoScmVwZWF0ZWQubWluX2l0ZW1zGmd1aW50KHRoaXMuc2l6ZSgpKSA8IHJ1bGVzLm1pbl9pdGVtcyA/ICdtdXN0IGNvbnRhaW4gYXQgbGVhc3QgJWQgaXRlbShzKScuZm9ybWF0KFtydWxlcy5taW5faXRlbXNdKSA6ICcnEpwBCgltYXhfaXRlbXMYAiABKARCiAHCSIQBCoEBChJyZXBlYXRlZC5tYXhfaXRlbXMaa3VpbnQodGhpcy5zaXplKCkpID4gcnVsZXMubWF4X2l0ZW1zID8gJ211c3QgY29udGFpbiBubyBtb3JlIHRoYW4gJXMgaXRlbShzKScuZm9ybWF0KFtydWxlcy5tYXhfaXRlbXNdKSA6ICcnEnAKBnVuaXF1ZRgDIAEoCEJgwkhdClsKD3JlcGVhdGVkLnVuaXF1ZRIocmVwZWF0ZWQgdmFsdWUgbXVzdCBjb250YWluIHVuaXF1ZSBpdGVtcxoeIXJ1bGVzLnVuaXF1ZSB8fCB0aGlzLnVuaXF1ZSgpEicKBWl0ZW1zGAQgASgLMhguYnVmLnZhbGlkYXRlLkZpZWxkUnVsZXMqCQjoBxCAgICAAiKKAwoITWFwUnVsZXMSjwEKCW1pbl9wYWlycxgBIAEoBEJ8wkh5CncKDW1hcC5taW5fcGFpcnMaZnVpbnQodGhpcy5zaXplKCkpIDwgcnVsZXMubWluX3BhaXJzID8gJ21hcCBtdXN0IGJlIGF0IGxlYXN0ICVkIGVudHJpZXMnLmZvcm1hdChbcnVsZXMubWluX3BhaXJzXSkgOiAnJxKOAQoJbWF4X3BhaXJzGAIgASgEQnvCSHgKdgoNbWFwLm1heF9wYWlycxpldWludCh0aGlzLnNpemUoKSkgPiBydWxlcy5tYXhfcGFpcnMgPyAnbWFwIG11c3QgYmUgYXQgbW9zdCAlZCBlbnRyaWVzJy5mb3JtYXQoW3J1bGVzLm1heF9wYWlyc10pIDogJycSJgoEa2V5cxgEIAEoCzIYLmJ1Zi52YWxpZGF0ZS5GaWVsZFJ1bGVzEigKBnZhbHVlcxgFIAEoCzIYLmJ1Zi52YWxpZGF0ZS5GaWVsZFJ1bGVzKgkI6AcQgICAgAIiJgoIQW55UnVsZXMSCgoCaW4YAiADKAkSDgoGbm90X2luGAMgAygJIr8WCg1EdXJhdGlvblJ1bGVzEpsBCgVjb25zdBgCIAEoCzIZLmdvb2dsZS5wcm90b2J1Zi5EdXJhdGlvbkJxwkhuCmwKDmR1cmF0aW9uLmNvbnN0Glp0aGlzICE9IGdldEZpZWxkKHJ1bGVzLCAnY29uc3QnKSA/ICdtdXN0IGVxdWFsICVzJy5mb3JtYXQoW2dldEZpZWxkKHJ1bGVzLCAnY29uc3QnKV0pIDogJycSogEKAmx0GAMgASgLMhkuZ29vZ2xlLnByb3RvYnVmLkR1cmF0aW9uQnnCSHYKdAoLZHVyYXRpb24ubHQaZSFoYXMocnVsZXMuZ3RlKSAmJiAhaGFzKHJ1bGVzLmd0KSAmJiB0aGlzID49IHJ1bGVzLmx0PyAnbXVzdCBiZSBsZXNzIHRoYW4gJXMnLmZvcm1hdChbcnVsZXMubHRdKSA6ICcnSAAStAEKA2x0ZRgEIAEoCzIZLmdvb2dsZS5wcm90b2J1Zi5EdXJhdGlvbkKJAcJIhQEKggEKDGR1cmF0aW9uLmx0ZRpyIWhhcyhydWxlcy5ndGUpICYmICFoYXMocnVsZXMuZ3QpICYmIHRoaXMgPiBydWxlcy5sdGU/ICdtdXN0IGJlIGxlc3MgdGhhbiBvciBlcXVhbCB0byAlcycuZm9ybWF0KFtydWxlcy5sdGVdKSA6ICcnSAASowcKAmd0GAUgASgLMhkuZ29vZ2xlLnByb3RvYnVmLkR1cmF0aW9uQvkGwkj1Bgp3CgtkdXJhdGlvbi5ndBpoIWhhcyhydWxlcy5sdCkgJiYgIWhhcyhydWxlcy5sdGUpICYmIHRoaXMgPD0gcnVsZXMuZ3Q/ICdtdXN0IGJlIGdyZWF0ZXIgdGhhbiAlcycuZm9ybWF0KFtydWxlcy5ndF0pIDogJycKsAEKDmR1cmF0aW9uLmd0X2x0Gp0BaGFzKHJ1bGVzLmx0KSAmJiBydWxlcy5sdCA+PSBydWxlcy5ndCAmJiAodGhpcyA+PSBydWxlcy5sdCB8fCB0aGlzIDw9IHJ1bGVzLmd0KT8gJ211c3QgYmUgZ3JlYXRlciB0aGFuICVzIGFuZCBsZXNzIHRoYW4gJXMnLmZvcm1hdChbcnVsZXMuZ3QsIHJ1bGVzLmx0XSkgOiAnJwq4AQoYZHVyYXRpb24uZ3RfbHRfZXhjbHVzaXZlGpsBaGFzKHJ1bGVzLmx0KSAmJiBydWxlcy5sdCA8IHJ1bGVzLmd0ICYmIChydWxlcy5sdCA8PSB0aGlzICYmIHRoaXMgPD0gcnVsZXMuZ3QpPyAnbXVzdCBiZSBncmVhdGVyIHRoYW4gJXMgb3IgbGVzcyB0aGFuICVzJy5mb3JtYXQoW3J1bGVzLmd0LCBydWxlcy5sdF0pIDogJycKwAEKD2R1cmF0aW9uLmd0X2x0ZRqsAWhhcyhydWxlcy5sdGUpICYmIHJ1bGVzLmx0ZSA+PSBydWxlcy5ndCAmJiAodGhpcyA+IHJ1bGVzLmx0ZSB8fCB0aGlzIDw9IHJ1bGVzLmd0KT8gJ211c3QgYmUgZ3JlYXRlciB0aGFuICVzIGFuZCBsZXNzIHRoYW4gb3IgZXF1YWwgdG8gJXMnLmZvcm1hdChbcnVsZXMuZ3QsIHJ1bGVzLmx0ZV0pIDogJycKyAEKGWR1cmF0aW9uLmd0X2x0ZV9leGNsdXNpdmUaqgFoYXMocnVsZXMubHRlKSAmJiBydWxlcy5sdGUgPCBydWxlcy5ndCAmJiAocnVsZXMubHRlIDwgdGhpcyAmJiB0aGlzIDw9IHJ1bGVzLmd0KT8gJ211c3QgYmUgZ3JlYXRlciB0aGFuICVzIG9yIGxlc3MgdGhhbiBvciBlcXVhbCB0byAlcycuZm9ybWF0KFtydWxlcy5ndCwgcnVsZXMubHRlXSkgOiAnJ0gBEu8HCgNndGUYBiABKAsyGS5nb29nbGUucHJvdG9idWYuRHVyYXRpb25CxAfCSMAHCoUBCgxkdXJhdGlvbi5ndGUadSFoYXMocnVsZXMubHQpICYmICFoYXMocnVsZXMubHRlKSAmJiB0aGlzIDwgcnVsZXMuZ3RlPyAnbXVzdCBiZSBncmVhdGVyIHRoYW4gb3IgZXF1YWwgdG8gJXMnLmZvcm1hdChbcnVsZXMuZ3RlXSkgOiAnJwq/AQoPZHVyYXRpb24uZ3RlX2x0GqsBaGFzKHJ1bGVzLmx0KSAmJiBydWxlcy5sdCA+PSBydWxlcy5ndGUgJiYgKHRoaXMgPj0gcnVsZXMubHQgfHwgdGhpcyA8IHJ1bGVzLmd0ZSk/ICdtdXN0IGJlIGdyZWF0ZXIgdGhhbiBvciBlcXVhbCB0byAlcyBhbmQgbGVzcyB0aGFuICVzJy5mb3JtYXQoW3J1bGVzLmd0ZSwgcnVsZXMubHRdKSA6ICcnCscBChlkdXJhdGlvbi5ndGVfbHRfZXhjbHVzaXZlGqkBaGFzKHJ1bGVzLmx0KSAmJiBydWxlcy5sdCA8IHJ1bGVzLmd0ZSAmJiAocnVsZXMubHQgPD0gdGhpcyAmJiB0aGlzIDwgcnVsZXMuZ3RlKT8gJ211c3QgYmUgZ3JlYXRlciB0aGFuIG9yIGVxdWFsIHRvICVzIG9yIGxlc3MgdGhhbiAlcycuZm9ybWF0KFtydWxlcy5ndGUsIHJ1bGVzLmx0XSkgOiAnJwrPAQoQZHVyYXRpb24uZ3RlX2x0ZRq6AWhhcyhydWxlcy5sdGUpICYmIHJ1bGVzLmx0ZSA+PSBydWxlcy5ndGUgJiYgKHRoaXMgPiBydWxlcy5sdGUgfHwgdGhpcyA8IHJ1bGVzLmd0ZSk/ICdtdXN0IGJlIGdyZWF0ZXIgdGhhbiBvciBlcXVhbCB0byAlcyBhbmQgbGVzcyB0aGFuIG9yIGVxdWFsIHRvICVzJy5mb3JtYXQoW3J1bGVzLmd0ZSwgcnVsZXMubHRlXSkgOiAnJwrXAQoaZHVyYXRpb24uZ3RlX2x0ZV9leGNsdXNpdmUauAFoYXMocnVsZXMubHRlKSAmJiBydWxlcy5sdGUgPCBydWxlcy5ndGUgJiYgKHJ1bGVzLmx0ZSA8IHRoaXMgJiYgdGhpcyA8IHJ1bGVzLmd0ZSk/ICdtdXN0IGJlIGdyZWF0ZXIgdGhhbiBvciBlcXVhbCB0byAlcyBvciBsZXNzIHRoYW4gb3IgZXF1YWwgdG8gJXMnLmZvcm1hdChbcnVsZXMuZ3RlLCBydWxlcy5sdGVdKSA6ICcnSAESlwEKAmluGAcgAygLMhkuZ29vZ2xlLnByb3RvYnVmLkR1cmF0aW9uQnDCSG0KawoLZHVyYXRpb24uaW4aXCEodGhpcyBpbiBnZXRGaWVsZChydWxlcywgJ2luJykpID8gJ211c3QgYmUgaW4gbGlzdCAlcycuZm9ybWF0KFtnZXRGaWVsZChydWxlcywgJ2luJyldKSA6ICcnEo4BCgZub3RfaW4YCCADKAsyGS5nb29nbGUucHJvdG9idWYuRHVyYXRpb25CY8JIYApeCg9kdXJhdGlvbi5ub3RfaW4aS3RoaXMgaW4gcnVsZXMubm90X2luID8gJ211c3Qgbm90IGJlIGluIGxpc3QgJXMnLmZvcm1hdChbcnVsZXMubm90X2luXSkgOiAnJxJJCgdleGFtcGxlGAkgAygLMhkuZ29vZ2xlLnByb3RvYnVmLkR1cmF0aW9uQh3CSBoKGAoQZHVyYXRpb24uZXhhbXBsZRoEdHJ1ZSoJCOgHEICAgIACQgsKCWxlc3NfdGhhbkIOCgxncmVhdGVyX3RoYW4i6wUKDkZpZWxkTWFza1J1bGVzErkBCgVjb25zdBgBIAEoCzIaLmdvb2dsZS5wcm90b2J1Zi5GaWVsZE1hc2tCjQHCSIkBCoYBChBmaWVsZF9tYXNrLmNvbnN0GnJ0aGlzLnBhdGhzICE9IGdldEZpZWxkKHJ1bGVzLCAnY29uc3QnKS5wYXRocyA/ICdtdXN0IGVxdWFsIHBhdGhzICVzJy5mb3JtYXQoW2dldEZpZWxkKHJ1bGVzLCAnY29uc3QnKS5wYXRoc10pIDogJycS0wEKAmluGAIgAygJQsYBwkjCAQq/AQoNZmllbGRfbWFzay5pbhqtASF0aGlzLnBhdGhzLmFsbChwLCBwIGluIGdldEZpZWxkKHJ1bGVzLCAnaW4nKSB8fCBnZXRGaWVsZChydWxlcywgJ2luJykuZXhpc3RzKGYsIHAuc3RhcnRzV2l0aChmKycuJykpKSA/ICdtdXN0IG9ubHkgY29udGFpbiBwYXRocyBpbiAlcycuZm9ybWF0KFtnZXRGaWVsZChydWxlcywgJ2luJyldKSA6ICcnEu0BCgZub3RfaW4YAyADKAlC3AHCSNgBCtUBChFmaWVsZF9tYXNrLm5vdF9pbhq/ASF0aGlzLnBhdGhzLmFsbChwLCAhKHAgaW4gZ2V0RmllbGQocnVsZXMsICdub3RfaW4nKSB8fCBnZXRGaWVsZChydWxlcywgJ25vdF9pbicpLmV4aXN0cyhmLCBwLnN0YXJ0c1dpdGgoZisnLicpKSkpID8gJ211c3Qgbm90IGNvbnRhaW4gYW55IHBhdGhzIGluICVzJy5mb3JtYXQoW2dldEZpZWxkKHJ1bGVzLCAnbm90X2luJyldKSA6ICcnEkwKB2V4YW1wbGUYBCADKAsyGi5nb29nbGUucHJvdG9idWYuRmllbGRNYXNrQh/CSBwKGgoSZmllbGRfbWFzay5leGFtcGxlGgR0cnVlKgkI6AcQgICAgAIisBcKDlRpbWVzdGFtcFJ1bGVzEp0BCgVjb25zdBgCIAEoCzIaLmdvb2dsZS5wcm90b2J1Zi5UaW1lc3RhbXBCcsJIbwptCg90aW1lc3RhbXAuY29uc3QaWnRoaXMgIT0gZ2V0RmllbGQocnVsZXMsICdjb25zdCcpID8gJ211c3QgZXF1YWwgJXMnLmZvcm1hdChbZ2V0RmllbGQocnVsZXMsICdjb25zdCcpXSkgOiAnJxKkAQoCbHQYAyABKAsyGi5nb29nbGUucHJvdG9idWYuVGltZXN0YW1wQnrCSHcKdQoMdGltZXN0YW1wLmx0GmUhaGFzKHJ1bGVzLmd0ZSkgJiYgIWhhcyhydWxlcy5ndCkgJiYgdGhpcyA+PSBydWxlcy5sdD8gJ211c3QgYmUgbGVzcyB0aGFuICVzJy5mb3JtYXQoW3J1bGVzLmx0XSkgOiAnJ0gAErYBCgNsdGUYBCABKAsyGi5nb29nbGUucHJvdG9idWYuVGltZXN0YW1wQooBwkiGAQqDAQoNdGltZXN0YW1wLmx0ZRpyIWhhcyhydWxlcy5ndGUpICYmICFoYXMocnVsZXMuZ3QpICYmIHRoaXMgPiBydWxlcy5sdGU/ICdtdXN0IGJlIGxlc3MgdGhhbiBvciBlcXVhbCB0byAlcycuZm9ybWF0KFtydWxlcy5sdGVdKSA6ICcnSAASZgoGbHRfbm93GAcgASgIQlTCSFEKTwoQdGltZXN0YW1wLmx0X25vdxo7KHJ1bGVzLmx0X25vdyAmJiB0aGlzID4gbm93KSA/ICdtdXN0IGJlIGxlc3MgdGhhbiBub3cnIDogJydIABKpBwoCZ3QYBSABKAsyGi5nb29nbGUucHJvdG9idWYuVGltZXN0YW1wQv4Gwkj6Bgp4Cgx0aW1lc3RhbXAuZ3QaaCFoYXMocnVsZXMubHQpICYmICFoYXMocnVsZXMubHRlKSAmJiB0aGlzIDw9IHJ1bGVzLmd0PyAnbXVzdCBiZSBncmVhdGVyIHRoYW4gJXMnLmZvcm1hdChbcnVsZXMuZ3RdKSA6ICcnCrEBCg90aW1lc3RhbXAuZ3RfbHQanQFoYXMocnVsZXMubHQpICYmIHJ1bGVzLmx0ID49IHJ1bGVzLmd0ICYmICh0aGlzID49IHJ1bGVzLmx0IHx8IHRoaXMgPD0gcnVsZXMuZ3QpPyAnbXVzdCBiZSBncmVhdGVyIHRoYW4gJXMgYW5kIGxlc3MgdGhhbiAlcycuZm9ybWF0KFtydWxlcy5ndCwgcnVsZXMubHRdKSA6ICcnCrkBChl0aW1lc3RhbXAuZ3RfbHRfZXhjbHVzaXZlGpsBaGFzKHJ1bGVzLmx0KSAmJiBydWxlcy5sdCA8IHJ1bGVzLmd0ICYmIChydWxlcy5sdCA8PSB0aGlzICYmIHRoaXMgPD0gcnVsZXMuZ3QpPyAnbXVzdCBiZSBncmVhdGVyIHRoYW4gJXMgb3IgbGVzcyB0aGFuICVzJy5mb3JtYXQoW3J1bGVzLmd0LCBydWxlcy5sdF0pIDogJycKwQEKEHRpbWVzdGFtcC5ndF9sdGUarAFoYXMocnVsZXMubHRlKSAmJiBydWxlcy5sdGUgPj0gcnVsZXMuZ3QgJiYgKHRoaXMgPiBydWxlcy5sdGUgfHwgdGhpcyA8PSBydWxlcy5ndCk/ICdtdXN0IGJlIGdyZWF0ZXIgdGhhbiAlcyBhbmQgbGVzcyB0aGFuIG9yIGVxdWFsIHRvICVzJy5mb3JtYXQoW3J1bGVzLmd0LCBydWxlcy5sdGVdKSA6ICcnCskBChp0aW1lc3RhbXAuZ3RfbHRlX2V4Y2x1c2l2ZRqqAWhhcyhydWxlcy5sdGUpICYmIHJ1bGVzLmx0ZSA8IHJ1bGVzLmd0ICYmIChydWxlcy5sdGUgPCB0aGlzICYmIHRoaXMgPD0gcnVsZXMuZ3QpPyAnbXVzdCBiZSBncmVhdGVyIHRoYW4gJXMgb3IgbGVzcyB0aGFuIG9yIGVxdWFsIHRvICVzJy5mb3JtYXQoW3J1bGVzLmd0LCBydWxlcy5sdGVdKSA6ICcnSAES9QcKA2d0ZRgGIAEoCzIaLmdvb2dsZS5wcm90b2J1Zi5UaW1lc3RhbXBCyQfCSMUHCoYBCg10aW1lc3RhbXAuZ3RlGnUhaGFzKHJ1bGVzLmx0KSAmJiAhaGFzKHJ1bGVzLmx0ZSkgJiYgdGhpcyA8IHJ1bGVzLmd0ZT8gJ211c3QgYmUgZ3JlYXRlciB0aGFuIG9yIGVxdWFsIHRvICVzJy5mb3JtYXQoW3J1bGVzLmd0ZV0pIDogJycKwAEKEHRpbWVzdGFtcC5ndGVfbHQaqwFoYXMocnVsZXMubHQpICYmIHJ1bGVzLmx0ID49IHJ1bGVzLmd0ZSAmJiAodGhpcyA+PSBydWxlcy5sdCB8fCB0aGlzIDwgcnVsZXMuZ3RlKT8gJ211c3QgYmUgZ3JlYXRlciB0aGFuIG9yIGVxdWFsIHRvICVzIGFuZCBsZXNzIHRoYW4gJXMnLmZvcm1hdChbcnVsZXMuZ3RlLCBydWxlcy5sdF0pIDogJycKyAEKGnRpbWVzdGFtcC5ndGVfbHRfZXhjbHVzaXZlGqkBaGFzKHJ1bGVzLmx0KSAmJiBydWxlcy5sdCA8IHJ1bGVzLmd0ZSAmJiAocnVsZXMubHQgPD0gdGhpcyAmJiB0aGlzIDwgcnVsZXMuZ3RlKT8gJ211c3QgYmUgZ3JlYXRlciB0aGFuIG9yIGVxdWFsIHRvICVzIG9yIGxlc3MgdGhhbiAlcycuZm9ybWF0KFtydWxlcy5ndGUsIHJ1bGVzLmx0XSkgOiAnJwrQAQoRdGltZXN0YW1wLmd0ZV9sdGUaugFoYXMocnVsZXMubHRlKSAmJiBydWxlcy5sdGUgPj0gcnVsZXMuZ3RlICYmICh0aGlzID4gcnVsZXMubHRlIHx8IHRoaXMgPCBydWxlcy5ndGUpPyAnbXVzdCBiZSBncmVhdGVyIHRoYW4gb3IgZXF1YWwgdG8gJXMgYW5kIGxlc3MgdGhhbiBvciBlcXVhbCB0byAlcycuZm9ybWF0KFtydWxlcy5ndGUsIHJ1bGVzLmx0ZV0pIDogJycK2AEKG3RpbWVzdGFtcC5ndGVfbHRlX2V4Y2x1c2l2ZRq4AWhhcyhydWxlcy5sdGUpICYmIHJ1bGVzLmx0ZSA8IHJ1bGVzLmd0ZSAmJiAocnVsZXMubHRlIDwgdGhpcyAmJiB0aGlzIDwgcnVsZXMuZ3RlKT8gJ211c3QgYmUgZ3JlYXRlciB0aGFuIG9yIGVxdWFsIHRvICVzIG9yIGxlc3MgdGhhbiBvciBlcXVhbCB0byAlcycuZm9ybWF0KFtydWxlcy5ndGUsIHJ1bGVzLmx0ZV0pIDogJydIARJpCgZndF9ub3cYCCABKAhCV8JIVApSChB0aW1lc3RhbXAuZ3Rfbm93Gj4ocnVsZXMuZ3Rfbm93ICYmIHRoaXMgPCBub3cpID8gJ211c3QgYmUgZ3JlYXRlciB0aGFuIG5vdycgOiAnJ0gBErEBCgZ3aXRoaW4YCSABKAsyGS5nb29nbGUucHJvdG9idWYuRHVyYXRpb25ChQHCSIEBCn8KEHRpbWVzdGFtcC53aXRoaW4aa3RoaXMgPCBub3ctcnVsZXMud2l0aGluIHx8IHRoaXMgPiBub3crcnVsZXMud2l0aGluID8gJ211c3QgYmUgd2l0aGluICVzIG9mIG5vdycuZm9ybWF0KFtydWxlcy53aXRoaW5dKSA6ICcnEksKB2V4YW1wbGUYCiADKAsyGi5nb29nbGUucHJvdG9idWYuVGltZXN0YW1wQh7CSBsKGQoRdGltZXN0YW1wLmV4YW1wbGUaBHRydWUqCQjoBxCAgICAAkILCglsZXNzX3RoYW5CDgoMZ3JlYXRlcl90aGFuIjkKClZpb2xhdGlvbnMSKwoKdmlvbGF0aW9ucxgBIAMoCzIXLmJ1Zi52YWxpZGF0ZS5WaW9sYXRpb24inwEKCVZpb2xhdGlvbhImCgVmaWVsZBgFIAEoCzIXLmJ1Zi52YWxpZGF0ZS5GaWVsZFBhdGgSJQoEcnVsZRgGIAEoCzIXLmJ1Zi52YWxpZGF0ZS5GaWVsZFBhdGgSDwoHcnVsZV9pZBgCIAEoCRIPCgdtZXNzYWdlGAMgASgJEg8KB2Zvcl9rZXkYBCABKAhKBAgBEAJSCmZpZWxkX3BhdGgiPQoJRmllbGRQYXRoEjAKCGVsZW1lbnRzGAEgAygLMh4uYnVmLnZhbGlkYXRlLkZpZWxkUGF0aEVsZW1lbnQi6QIKEEZpZWxkUGF0aEVsZW1lbnQSFAoMZmllbGRfbnVtYmVyGAEgASgFEhIKCmZpZWxkX25hbWUYAiABKAkSPgoKZmllbGRfdHlwZRgDIAEoDjIqLmdvb2dsZS5wcm90b2J1Zi5GaWVsZERlc2NyaXB0b3JQcm90by5UeXBlEjwKCGtleV90eXBlGAQgASgOMiouZ29vZ2xlLnByb3RvYnVmLkZpZWxkRGVzY3JpcHRvclByb3RvLlR5cGUSPgoKdmFsdWVfdHlwZRgFIAEoDjIqLmdvb2dsZS5wcm90b2J1Zi5GaWVsZERlc2NyaXB0b3JQcm90by5UeXBlEg8KBWluZGV4GAYgASgESAASEgoIYm9vbF9rZXkYByABKAhIABIRCgdpbnRfa2V5GAggASgDSAASEgoIdWludF9rZXkYCSABKARIABIUCgpzdHJpbmdfa2V5GAogASgJSABCCwoJc3Vic2NyaXB0KqEBCgZJZ25vcmUSFgoSSUdOT1JFX1VOU1BFQ0lGSUVEEAASGAoUSUdOT1JFX0lGX1pFUk9fVkFMVUUQARIRCg1JR05PUkVfQUxXQVlTEAMiBAgCEAIqDElHTk9SRV9FTVBUWSoOSUdOT1JFX0RFRkFVTFQqF0lHTk9SRV9JRl9ERUZBVUxUX1ZBTFVFKhVJR05PUkVfSUZfVU5QT1BVTEFURUQqbgoKS25vd25SZWdleBIbChdLTk9XTl9SRUdFWF9VTlNQRUNJRklFRBAAEiAKHEtOT1dOX1JFR0VYX0hUVFBfSEVBREVSX05BTUUQARIhCh1LTk9XTl9SRUdFWF9IVFRQX0hFQURFUl9WQUxVRRACOlYKB21lc3NhZ2USHy5nb29nbGUucHJvdG9idWYuTWVzc2FnZU9wdGlvbnMYhwkgASgLMhouYnVmLnZhbGlkYXRlLk1lc3NhZ2VSdWxlc1IHbWVzc2FnZTpOCgVvbmVvZhIdLmdvb2dsZS5wcm90b2J1Zi5PbmVvZk9wdGlvbnMYhwkgASgLMhguYnVmLnZhbGlkYXRlLk9uZW9mUnVsZXNSBW9uZW9mOk4KBWZpZWxkEh0uZ29vZ2xlLnByb3RvYnVmLkZpZWxkT3B0aW9ucxiHCSABKAsyGC5idWYudmFsaWRhdGUuRmllbGRSdWxlc1IFZmllbGQ6XQoKcHJlZGVmaW5lZBIdLmdvb2dsZS5wcm90b2J1Zi5GaWVsZE9wdGlvbnMYiAkgASgLMh0uYnVmLnZhbGlkYXRlLlByZWRlZmluZWRSdWxlc1IKcHJlZGVmaW5lZEJuChJidWlsZC5idWYudmFsaWRhdGVCDVZhbGlkYXRlUHJvdG9QAVpHYnVmLmJ1aWxkL2dlbi9nby9idWZidWlsZC9wcm90b3ZhbGlkYXRlL3Byb3RvY29sYnVmZmVycy9nby9idWYvdmFsaWRhdGU", [file_google_protobuf_descriptor, file_google_protobuf_duration, file_google_protobuf_field_mask, file_google_protobuf_timestamp]);

// packages/connect-client/src/gen/tammy/v1/common_pb.ts
var file_tammy_v1_common = /* @__PURE__ */ fileDesc("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ggECEAESZAoOY29ycmVsYXRpb25faWQYBiABKAlCTLpISXJHMkVeWzAtOWEtZl17OH0tWzAtOWEtZl17NH0tN1swLTlhLWZdezN9LVs4OWFiXVswLTlhLWZdezN9LVswLTlhLWZdezEyfSQibgoORmllbGRWaW9sYXRpb24SHgoKZmllbGRfcGF0aBgBIAEoCUIKukgHcgUQARiAAhIaCgdydWxlX2lkGAIgASgJQgm6SAZyBBABGGASIAoMc2FmZV9tZXNzYWdlGAMgASgJQgq6SAdyBRABGIACIoABChVWYWxpZGF0aW9uRXJyb3JEZXRhaWwSLwoHY29udGV4dBgBIAEoCzIWLnRhbW15LnYxLkVycm9yQ29udGV4dEIGukgDyAEBEjYKCnZpb2xhdGlvbnMYAiADKAsyGC50YW1teS52MS5GaWVsZFZpb2xhdGlvbkIIukgFkgECEEAimQEKGUF1dGhlbnRpY2F0aW9uRXJyb3JEZXRhaWwSLwoHY29udGV4dBgBIAEoCzIWLnRhbW15LnYxLkVycm9yQ29udGV4dEIGukgDyAEBEhoKCWNoYWxsZW5nZRgCIAEoCUIHukgEcgIYQBIvCgtyZXRyeV9hZnRlchgDIAEoCzIaLmdvb2dsZS5wcm90b2J1Zi5UaW1lc3RhbXAidAobUGVybWlzc2lvbkRlbmllZEVycm9yRGV0YWlsEi8KB2NvbnRleHQYASABKAsyFi50YW1teS52MS5FcnJvckNvbnRleHRCBrpIA8gBARIkChNyZXF1aXJlZF9wZXJtaXNzaW9uGAIgASgJQge6SARyAhhgImsKHkZyZXNoRmFjdG9yUmVxdWlyZWRFcnJvckRldGFpbBIvCgdjb250ZXh0GAEgASgLMhYudGFtbXkudjEuRXJyb3JDb250ZXh0Qga6SAPIAQESGAoHcHVycG9zZRgCIAEoCUIHukgEcgIYYCL7AQoXU3RhbGVWZXJzaW9uRXJyb3JEZXRhaWwSLwoHY29udGV4dBgBIAEoCzIWLnRhbW15LnYxLkVycm9yQ29udGV4dEIGukgDyAEBEhkKCHJlc291cmNlGAIgASgJQge6SARyAhhAEmEKC3Jlc291cmNlX2lkGAMgASgJQky6SElyRzJFXlswLTlhLWZdezh9LVswLTlhLWZdezR9LTdbMC05YS1mXXszfS1bODlhYl1bMC05YS1mXXszfS1bMC05YS1mXXsxMn0kEhgKEGV4cGVjdGVkX3ZlcnNpb24YBCABKAQSFwoPY3VycmVudF92ZXJzaW9uGAUgASgEIrUBCiFJbnZhbGlkU3RhdGVUcmFuc2l0aW9uRXJyb3JEZXRhaWwSLwoHY29udGV4dBgBIAEoCzIWLnRhbW15LnYxLkVycm9yQ29udGV4dEIGukgDyAEBEhkKCHJlc291cmNlGAIgASgJQge6SARyAhhAEh4KDWN1cnJlbnRfc3RhdGUYAyABKAlCB7pIBHICGGASJAoSYWxsb3dlZF9vcGVyYXRpb25zGAQgAygJQgi6SAWSAQIQICLYAQoeSWRlbXBvdGVuY3lDb25mbGljdEVycm9yRGV0YWlsEi8KB2NvbnRleHQYASABKAsyFi50YW1teS52MS5FcnJvckNvbnRleHRCBrpIA8gBARJlCg9pZGVtcG90ZW5jeV9rZXkYAiABKAlCTLpISXJHMkVeWzAtOWEtZl17OH0tWzAtOWEtZl17NH0tN1swLTlhLWZdezN9LVs4OWFiXVswLTlhLWZdezN9LVswLTlhLWZdezEyfSQSHgoMcmVxdWVzdF90eXBlGAMgASgJQgi6SAVyAxjAASLmAQoaRHVwbGljYXRlU291cmNlRXJyb3JEZXRhaWwSLwoHY29udGV4dBgBIAEoCzIWLnRhbW15LnYxLkVycm9yQ29udGV4dEIGukgDyAEBEisKBnNvdXJjZRgCIAEoCzITLnRhbW15LnYxLlNvdXJjZVJlZkIGukgDyAEBEmoKFGV4aXN0aW5nX3Jlc291cmNlX2lkGAMgASgJQky6SElyRzJFXlswLTlhLWZdezh9LVswLTlhLWZdezR9LTdbMC05YS1mXXszfS1bODlhYl1bMC05YS1mXXszfS1bMC05YS1mXXsxMn0kItwBChdDbG9zZWRQZXJpb2RFcnJvckRldGFpbBIvCgdjb250ZXh0GAEgASgLMhYudGFtbXkudjEuRXJyb3JDb250ZXh0Qga6SAPIAQESXwoJcGVyaW9kX2lkGAIgASgJQky6SElyRzJFXlswLTlhLWZdezh9LVswLTlhLWZdezR9LTdbMC05YS1mXXszfS1bODlhYl1bMC05YS1mXXszfS1bMC05YS1mXXsxMn0kEi8KCnBlcmlvZF9lbmQYAyABKAsyEy50YW1teS52MS5DaXZpbERhdGVCBrpIA8gBASKaAQoUSW1iYWxhbmNlRXJyb3JEZXRhaWwSLwoHY29udGV4dBgBIAEoCzIWLnRhbW15LnYxLkVycm9yQ29udGV4dEIGukgDyAEBEicKBmRlYml0cxgCIAEoCzIPLnRhbW15LnYxLk1vbmV5Qga6SAPIAQESKAoHY3JlZGl0cxgDIAEoCzIPLnRhbW15LnYxLk1vbmV5Qga6SAPIAQEi2AEKHUV2aWRlbmNlQ29ycnVwdGlvbkVycm9yRGV0YWlsEi8KB2NvbnRleHQYASABKAsyFi50YW1teS52MS5FcnJvckNvbnRleHRCBrpIA8gBARJhCgtldmlkZW5jZV9pZBgCIAEoCUJMukhJckcyRV5bMC05YS1mXXs4fS1bMC05YS1mXXs0fS03WzAtOWEtZl17M30tWzg5YWJdWzAtOWEtZl17M30tWzAtOWEtZl17MTJ9JBIjChJ2ZXJpZmljYXRpb25fc3RhZ2UYAyABKAlCB7pIBHICGEAiUwoLSm9iUHJvZ3Jlc3MSFwoPY29tcGxldGVkX3VuaXRzGAEgASgEEhMKC3RvdGFsX3VuaXRzGAIgASgEEhYKBXN0YWdlGAMgASgJQge6SARyAhhAKukBCg1FcnJvckNhdGVnb3J5Eh4KGkVSUk9SX0NBVEVHT1JZX1VOU1BFQ0lGSUVEEAASHQoZRVJST1JfQ0FURUdPUllfVkFMSURBVElPThABEiEKHUVSUk9SX0NBVEVHT1JZX0FVVEhFTlRJQ0FUSU9OEAISIAocRVJST1JfQ0FURUdPUllfQVVUSE9SSVNBVElPThADEhsKF0VSUk9SX0NBVEVHT1JZX0NPTkZMSUNUEAQSGgoWRVJST1JfQ0FURUdPUllfU1RPUkFHRRAFEhsKF0VSUk9SX0NBVEVHT1JZX0lOVEVSTkFMEAYqpgEKE1JldHJ5Q2xhc3NpZmljYXRpb24SJAogUkVUUllfQ0xBU1NJRklDQVRJT05fVU5TUEVDSUZJRUQQABIeChpSRVRSWV9DTEFTU0lGSUNBVElPTl9ORVZFUhABEioKJlJFVFJZX0NMQVNTSUZJQ0FUSU9OX0FGVEVSX1VTRVJfQUNUSU9OEAISHQoZUkVUUllfQ0xBU1NJRklDQVRJT05fU0FGRRADQkdaRWdpdGh1Yi5jb20vdGFtbXlhcHAvdGFtbXkvc2VydmljZXMvY29yZS9pbnRlcm5hbC9nZW4vdGFtbXkvdjE7dGFtbXl2MWIGcHJvdG8z", [file_buf_validate_validate, file_google_protobuf_timestamp]);

// packages/connect-client/src/gen/tammy/v1/documents_pb.ts
var file_tammy_v1_documents = /* @__PURE__ */ fileDesc("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KwoEcGFnZRgDIAEoCzIVLnRhbW15LnYxLlBhZ2VSZXF1ZXN0Qga6SAPIAQEicwoVTGlzdERvY3VtZW50c1Jlc3BvbnNlEjAKCWRvY3VtZW50cxgBIAMoCzISLnRhbW15LnYxLkRvY3VtZW50Qgm6SAaSAQMQyAESKAoEcGFnZRgCIAEoCzISLnRhbW15LnYxLlBhZ2VJbmZvQga6SAPIAQEilAIKGVNhdmVEb2N1bWVudFJldmlld1JlcXVlc3QSOQoPY29tbWFuZF9jb250ZXh0GAEgASgLMhgudGFtbXkudjEuQ29tbWFuZENvbnRleHRCBrpIA8gBARJhCgtkb2N1bWVudF9pZBgCIAEoCUJMukhJckcyRV5bMC05YS1mXXs4fS1bMC05YS1mXXs0fS03WzAtOWEtZl17M30tWzg5YWJdWzAtOWEtZl17M30tWzAtOWEtZl17MTJ9JBIhChBleHBlY3RlZF92ZXJzaW9uGAMgASgEQge6SAQyAigBEjYKCWNhbmRpZGF0ZRgEIAEoCzIbLnRhbW15LnYxLkRvY3VtZW50Q2FuZGlkYXRlQga6SAPIAQEiSgoaU2F2ZURvY3VtZW50UmV2aWV3UmVzcG9uc2USLAoIZG9jdW1lbnQYASABKAsyEi50YW1teS52MS5Eb2N1bWVudEIGukgDyAEBKnEKDkRvY3VtZW50U3RhdHVzEh8KG0RPQ1VNRU5UX1NUQVRVU19VTlNQRUNJRklFRBAAEiAKHERPQ1VNRU5UX1NUQVRVU19ORUVEU19SRVZJRVcQARIcChhET0NVTUVOVF9TVEFUVVNfUkVWSUVXRUQQAjLlAgoPRG9jdW1lbnRTZXJ2aWNlElMKDkluZ2VzdERvY3VtZW50Eh8udGFtbXkudjEuSW5nZXN0RG9jdW1lbnRSZXF1ZXN0GiAudGFtbXkudjEuSW5nZXN0RG9jdW1lbnRSZXNwb25zZRJKCgtHZXREb2N1bWVudBIcLnRhbW15LnYxLkdldERvY3VtZW50UmVxdWVzdBodLnRhbW15LnYxLkdldERvY3VtZW50UmVzcG9uc2USUAoNTGlzdERvY3VtZW50cxIeLnRhbW15LnYxLkxpc3REb2N1bWVudHNSZXF1ZXN0Gh8udGFtbXkudjEuTGlzdERvY3VtZW50c1Jlc3BvbnNlEl8KElNhdmVEb2N1bWVudFJldmlldxIjLnRhbW15LnYxLlNhdmVEb2N1bWVudFJldmlld1JlcXVlc3QaJC50YW1teS52MS5TYXZlRG9jdW1lbnRSZXZpZXdSZXNwb25zZUJHWkVnaXRodWIuY29tL3RhbW15YXBwL3RhbW15L3NlcnZpY2VzL2NvcmUvaW50ZXJuYWwvZ2VuL3RhbW15L3YxO3RhbW15djFiBnByb3RvMw", [file_buf_validate_validate, file_google_protobuf_timestamp, file_tammy_v1_common]);
var IngestDocumentRequestSchema = /* @__PURE__ */ messageDesc(file_tammy_v1_documents, 2);
var IngestDocumentResponseSchema = /* @__PURE__ */ messageDesc(file_tammy_v1_documents, 3);
var ListDocumentsRequestSchema = /* @__PURE__ */ messageDesc(file_tammy_v1_documents, 6);
var ListDocumentsResponseSchema = /* @__PURE__ */ messageDesc(file_tammy_v1_documents, 7);
var SaveDocumentReviewRequestSchema = /* @__PURE__ */ messageDesc(file_tammy_v1_documents, 8);
var SaveDocumentReviewResponseSchema = /* @__PURE__ */ messageDesc(file_tammy_v1_documents, 9);

// packages/connect-client/src/gen/tammy/v1/accounting_pb.ts
var file_tammy_v1_accounting = /* @__PURE__ */ fileDesc("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bExpbmVJbnB1dBJkCg5jbGllbnRfbGluZV9pZBgBIAEoCUJMukhJckcyRV5bMC05YS1mXXs4fS1bMC05YS1mXXs0fS03WzAtOWEtZl17M30tWzg5YWJdWzAtOWEtZl17M30tWzAtOWEtZl17MTJ9JBJgCgphY2NvdW50X2lkGAIgASgJQky6SElyRzJFXlswLTlhLWZdezh9LVswLTlhLWZdezR9LTdbMC05YS1mXXszfS1bODlhYl1bMC05YS1mXXszfS1bMC05YS1mXXsxMn0kEiYKBWRlYml0GAMgASgLMg8udGFtbXkudjEuTW9uZXlCBrpIA8gBARInCgZjcmVkaXQYBCABKAsyDy50YW1teS52MS5Nb25leUIGukgDyAEBEh0KC2Rlc2NyaXB0aW9uGAUgASgJQgi6SAVyAxiABBJmCgt0YXhfY29kZV9pZBgGIAEoCUJMukhJckcyRV5bMC05YS1mXXs4fS1bMC05YS1mXXs0fS03WzAtOWEtZl17M30tWzg5YWJdWzAtOWEtZl17M30tWzAtOWEtZl17MTJ9JEgAiAEBEiMKCnRheF9hbW91bnQYByABKAsyDy50YW1teS52MS5Nb25leRIjCgZzb3VyY2UYCCABKAsyEy50YW1teS52MS5Tb3VyY2VSZWZCDgoMX3RheF9jb2RlX2lkIvoECgtKb3VybmFsTGluZRJYCgJpZBgBIAEoCUJMukhJckcyRV5bMC05YS1mXXs4fS1bMC05YS1mXXs0fS03WzAtOWEtZl17M30tWzg5YWJdWzAtOWEtZl17M30tWzAtOWEtZl17MTJ9JBJgCgpqb3VybmFsX2lkGAIgASgJQky6SElyRzJFXlswLTlhLWZdezh9LVswLTlhLWZdezR9LTdbMC05YS1mXXszfS1bODlhYl1bMC05YS1mXXszfS1bMC05YS1mXXsxMn0kEmAKCmFjY291bnRfaWQYAyABKAlCTLpISXJHMkVeWzAtOWEtZl17OH0tWzAtOWEtZl17NH0tN1swLTlhLWZdezN9LVs4OWFiXVswLTlhLWZdezN9LVswLTlhLWZdezEyfSQSGQoIc2VxdWVuY2UYBCABKA1CB7pIBCoCKAESJgoFZGViaXQYBSABKAsyDy50YW1teS52MS5Nb25leUIGukgDyAEBEicKBmNyZWRpdBgGIAEo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dGF0dXMYBCABKA4yFy50YW1teS52MS5BY2NvdW50U3RhdHVzQgi6SAWCAQIQARIaCgZyZWFzb24YBSABKAlCCrpIB3IFEAEYgAQiRgoYU2V0QWNjb3VudFN0YXR1c1Jlc3BvbnNlEioKB2FjY291bnQYASABKAsyES50YW1teS52MS5BY2NvdW50Qga6SAPIAQEinQIKE0xpc3RUYXhDb2Rlc1JlcXVlc3QSPwoOYXV0aGVudGljYXRpb24YASABKAsyHy50YW1teS52MS5BdXRoZW50aWNhdGlvbkNvbnRleHRCBrpIA8gBARJlCg9vcmdhbmlzYXRpb25faWQYAiABKAlCTLpISXJHMkVeWzAtOWEtZl17OH0tWzAtOWEtZl17NH0tN1swLTlhLWZdezN9LVs4OWFiXVswLTlhLWZdezN9LVswLTlhLWZdezEyfSQSMQoMcG9zdGluZ19kYXRlGAMgASgLMhMudGFtbXkudjEuQ2l2aWxEYXRlQga6SAPIAQESKwoEcGFnZRgEIAEoCzIVLnRhbW15LnYxLlBhZ2VSZXF1ZXN0Qga6SAPIAQEicQoUTGlzdFRheENvZGVzUmVzcG9uc2USLwoJdGF4X2NvZGVzGAEgAygLMhEudGFtbXkudjEuVGF4Q29kZUIJukgGkgEDEMgBEigKBHBhZ2UYAiABKAsyEi50YW1teS52MS5QYWdlSW5mb0IGukgDyAEBIv4CChxQb3N0T3BlbmluZ0NvbnZlcnNpb25SZXF1ZXN0EjkKD2NvbW1hbmRfY29udGV4dBgBIAEoCzIYLnRhbW15LnYxLkNvbW1hbmRDb250ZXh0Qga6SAPIAQESZQoPb3JnYW5pc2F0aW9uX2lkGAIgASgJQky6SElyRzJFXlswLTlhLWZdezh9LVswLTlhLWZdezR9LTdbMC05YS1mXXszfS1bODlhYl1bMC05YS1mXXszfS1bMC05YS1mXXsxMn0kEjQKD2NvbnZlcnNpb25fZGF0ZRgDIAEoCzITLnRhbW15LnYxLkNpdmlsRGF0ZUIGukgDyAEBEjwKCGJhbGFuY2VzGAQgAygLMh0udGFtbXkudjEuT3BlbmluZ0JhbGFuY2VJbnB1dEILukgIkgEFCAIQiCcSKAobZXhwZWN0ZWRfZmluYW5jaWFsX3JldmlzaW9uGAUgASgESACIAQFCHgocX2V4cGVjdGVkX2ZpbmFuY2lhbF9yZXZpc2lvbiKEAQodUG9zdE9wZW5pbmdDb252ZXJzaW9uUmVzcG9uc2USNwoKY29udmVyc2lvbhgBIAEoCzIbLnRhbW15LnYxLk9wZW5pbmdDb252ZXJzaW9uQga6SAPIAQESKgoHam91cm5hbBgCIAEoCzIRLnRhbW15LnYxLkpvdXJuYWxCBrpIA8gBASL9AgofUmVwbGFjZU9wZW5pbmdDb252ZXJzaW9uUmVxdWVzdBI5Cg9jb21tYW5kX2NvbnRleHQYASABKAsyGC50YW1teS52MS5Db21tYW5kQ29udGV4dEIGukgDyAEBEmsKFW9wZW5pbmdfY29udmVyc2lvbl9pZBgCIAEoCUJMukhJckcyRV5bMC05YS1mXXs4fS1bMC05YS1mXXs0fS03WzAtOWEtZl17M30tWzg5YWJdWzAtOWEtZl17M30tWzAtOWEtZl17MTJ9JBIhChBleHBlY3RlZF92ZXJzaW9uGAMgASgEQge6SAQyAigBEjUKEHJlcGxhY2VtZW50X2RhdGUYBCABKAsyEy50YW1teS52MS5DaXZpbERhdGVCBrpIA8gBARI8CghiYWxhbmNlcxgFIAMoCzIdLnRhbW15LnYxLk9wZW5pbmdCYWxhbmNlSW5wdXRCC7pICJIBBQgCEIgnEhoKBnJlYXNvbhgGIAEoCUIKukgHcgUQARiABCKDAgogUmVwbGFjZU9wZW5pbmdDb252ZXJzaW9uUmVzcG9uc2USOAoLcHJlZGVjZXNzb3IYASABKAsyGy50YW1teS52MS5PcGVuaW5nQ29udmVyc2lvbkIGukgDyAEBEjgKC3JlcGxhY2VtZW50GAIgASgLMhsudGFtbXkudjEuT3BlbmluZ0NvbnZlcnNpb25CBrpIA8gBARIzChByZXZlcnNhbF9qb3VybmFsGAMgASgLMhEudGFtbXkudjEuSm91cm5hbEIGukgDyAEBEjYKE3JlcGxhY2VtZW50X2pvdXJuYWwYBCABKAsyES50YW1teS52MS5Kb3VybmFsQga6SAPIAQEijwMKGFBvc3RNYW51YWxKb3VybmFsUmVxdWVzdBI5Cg9jb21tYW5kX2NvbnRleHQYASABKAsyGC50YW1teS52MS5Db21tYW5kQ29udGV4dEIGukgDyAEBEmUKD29yZ2FuaXNhdGlvbl9pZBgCIAEoCUJMukhJckcyRV5bMC05YS1mXXs4fS1bMC05YS1mXXs0fS03WzAtOWEtZl17M30tWzg5YWJdWzAtOWEtZl17M30tWzAtOWEtZl17MTJ9JBIxCgxwb3N0aW5nX2RhdGUYAyABKAsyEy50YW1teS52MS5DaXZpbERhdGVCBrpIA8gBARIWCgRtZW1vGAQgASgJQgi6SAVyAxiABBI8CgVsaW5lcxgFIAMoCzIgLnRhbW15LnYxLk1hbnVhbEpvdXJuYWxMaW5lSW5wdXRCC7pICJIBBQgCEOgHEigKG2V4cGVjdGVkX2ZpbmFuY2lhbF9yZXZpc2lvbhgGIAEoBEgAiAEBQh4KHF9leHBlY3RlZF9maW5hbmNpYWxfcmV2aXNpb24iRwoZUG9zdE1hbnVhbEpvdXJuYWxSZXNwb25zZRIqCgdqb3VybmFsGAEgASgLMhEudGFtbXkudjEuSm91cm5hbEIGukgDyAEBIqcCChVSZXZlcnNlSm91cm5hbFJlcXVlc3QSOQoPY29tbWFuZF9jb250ZXh0GAEgASgLMhgudGFtbXkudjEuQ29tbWFuZENvbnRleHRCBrpIA8gBARJgCgpqb3VybmFsX2lkGAIgASgJQky6SElyRzJFXlswLTlhLWZdezh9LVswLTlhLWZdezR9LTdbMC05YS1mXXszfS1bODlhYl1bMC05YS1mXXszfS1bMC05YS1mXXsxMn0kEiEKEGV4cGVjdGVkX3ZlcnNpb24YAyABKARCB7pIBDICKAESMgoNcmV2ZXJzYWxfZGF0ZRgEIAEoCzITLnRhbW15LnYxLkNpdmlsRGF0ZUIGukgDyAEBEhoKBnJlYXNvbhgFIAEoCUIKukgHcgUQARiABCJyChZSZXZlcnNlSm91cm5hbFJlc3BvbnNlEisKCG9yaWdpbmFsGAEgASgLMhEudGFtbXkudjEuSm91cm5hbEIGukgDyAEBEisKCHJldmVyc2FsGAIgASgLMhEudGFtbXkudjEuSm91cm5hbEIGukgDyAEBIpMCChJDbG9zZVBlcmlvZFJlcXVlc3QSOQoPY29tbWFuZF9jb250ZXh0GAEgASgLMhgudGFtbXkudjEuQ29tbWFuZENvbnRleHRCBrpIA8gBARJlCg9vcmdhbmlzYXRp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ZXF1ZXN0Qga6SAPIAQESJwoKc3RhcnRfZGF0ZRgEIAEoCzITLnRhbW15LnYxLkNpdmlsRGF0ZRIlCghlbmRfZGF0ZRgFIAEoCzITLnRhbW15LnYxLkNpdmlsRGF0ZRIqCgVzdGF0ZRgGIAEoDjIWLnRhbW15LnYxLkpvdXJuYWxTdGF0ZUgAiAEBEiwKBnNvdXJjZRgHIAEoDjIXLnRhbW15LnYxLkpvdXJuYWxTb3VyY2VIAYgBAUIICgZfc3RhdGVCCQoHX3NvdXJjZSJwChRMaXN0Sm91cm5hbHNSZXNwb25zZRIuCghqb3VybmFscxgBIAMoCzIRLnRhbW15LnYxLkpvdXJuYWxCCbpIBpIBAxDIARIoCgRwYWdlGAIgASgLMhIudGFtbXkudjEuUGFnZUluZm9CBrpIA8gBASKrAwoXR2V0R2VuZXJhbExlZGdlclJlcXVlc3QSPwoOYXV0aGVudGljYXRpb24YASABKAsyHy50YW1teS52MS5BdXRoZW50aWNhdGlvbkNvbnRleHRCBrpIA8gBARJlCg9vcmdhbmlzYXRpb25faWQYAiABKAlCTLpISXJHMkVeWzAtOWEtZl17OH0tWzAtOWEtZl17NH0tN1swLTlhLWZdezN9LVs4OWFiXVswLTlhLWZdezN9LVswLTlhLWZdezEyfSQSawoLYWNjb3VudF9pZHMYAyADKAlCVrpIU5IBUBDIARgBIklyRzJFXlswLTlhLWZdezh9LVswLTlhLWZdezR9LTdbMC05YS1mXXszfS1bODlhYl1bMC05YS1mXXszfS1bMC05YS1mXXsxMn0kEicKCnN0YXJ0X2RhdGUYBCABKAsyEy50YW1teS52MS5DaXZpbERhdGUSJQoIZW5kX2RhdGUYBSABKAsyEy50YW1teS52MS5DaXZpbERhdGUSKwoEcGFnZRgGIAEoCzIVLnRhbW15LnYxLlBhZ2VSZXF1ZXN0Qga6SAPIAQEinAEKGEdldEdlbmVyYWxMZWRnZXJSZXNwb25zZRIxCgdlbnRyaWVzGAEgAygLMhUudGFtbXkudjEuTGVkZ2VyRW50cnlCCbpIBpIBAxDIARIoCgRwYWdlGAIgASgLMhIudGFtbXkudjEuUGFnZUluZm9CBrpIA8gBARIjChJmaW5hbmNpYWxfcmV2aXNpb24YAyABKARCB7pIBDICKAEiuwIKFkdldFRyaWFsQmFsYW5jZVJlcXVlc3QSPwoOYXV0aGVudGljYXRpb24YASABKAsyHy50YW1teS52MS5BdXRoZW50aWNhdGlvbkNvbnRleHRCBrpIA8gBARJlCg9vcmdhbmlzYXRpb25faWQYAiABKAlCTLpISXJHMkVeWzAtOWEtZl17OH0tWzAtOWEtZl17NH0tN1swLTlhLWZdezN9LVs4OWFiXVswLTlhLWZdezN9LVswLTlhLWZdezEyfSQSLwoKYXNfb2ZfZGF0ZRgDIAEoCzITLnRhbW15LnYxLkNpdmlsRGF0ZUIGukgDyAEBEigKG2V4cGVjdGVkX2ZpbmFuY2lhbF9yZXZpc2lvbhgEIAEoBEgAiAEBQh4KHF9leHBlY3RlZF9maW5hbmNpYWxfcmV2aXNpb24i0wEKF0dldFRyaWFsQmFsYW5jZVJlc3BvbnNlEjQKBWxpbmVzGAEgAygLMhoudGFtbXkudjEuVHJpYWxCYWxhbmNlTGluZUIJukgGkgEDEJBOEi0KDHRvdGFsX2RlYml0cxgCIAEoCzIPLnRhbW15LnYxLk1vbmV5Qga6SAPIAQESLgoNdG90YWxfY3JlZGl0cxgDIAEoCzIPLnRhbW15LnYxLk1vbmV5Qga6SAPIAQESIwoSZmluYW5jaWFsX3JldmlzaW9uGAQgASgEQge6SAQyAigBKoUCCgtBY2NvdW50VHlwZRIcChhBQ0NPVU5UX1RZUEVfVU5TUEVDSUZJRUQQABIWChJBQ0NPVU5UX1RZUEVfQVNTRVQQARIaChZBQ0NPVU5UX1RZUEVfTElBQklMSVRZEAISFwoTQUNDT1VOVF9UWVBFX0VRVUlUWRADEhgKFEFDQ09VTlRfVFlQRV9SRVZFTlVFEAQSHgoaQUNDT1VOVF9UWVBFX09USEVSX1JFVkVOVUUQBRIYChRBQ0NPVU5UX1RZUEVfRVhQRU5TRRAGEh4KGkFDQ09VTlRfVFlQRV9PVEhFUl9FWFBFTlNFEAcSFwoTQUNDT1VOVF9UWVBFX0NPTlRSQRAIKmQKDU5vcm1hbEJhbGFuY2USHgoaTk9STUFMX0JBTEFOQ0VfVU5TUEVDSUZJRUQQABIYChROT1JNQUxfQkFMQU5DRV9ERUJJVBABEhkKFU5PUk1BTF9CQUxBTkNFX0NSRURJVBACKmcKDUFjY291bnRTdGF0dXMSHgoaQUNDT1VOVF9TVEFUVVNfVU5TUEVDSUZJRUQQABIZChVBQ0NPVU5UX1NUQVRVU19BQ1RJVkUQARIbChdBQ0NPVU5UX1NUQVRVU19BUkNISVZFRBACKpwBChJBY2NvdW50RGVzaWduYXRpb24SIwofQUNDT1VOVF9ERVNJR05BVElPTl9VTlNQRUNJRklFRBAAEiAKHEFDQ09VTlRfREVTSUdOQVRJT05fT1JESU5BUlkQARIeChpBQ0NPVU5UX0RFU0lHTkFUSU9OX1NZU1RFTRACEh8KG0FDQ09VTlRfREVTSUdOQVRJT05fQ09OVFJPTBADKo4BChZPcGVuaW5nQ29udmVyc2lvblN0YXRlEigKJE9QRU5JTkdfQ09OVkVSU0lPTl9TVEFURV9VTlNQRUNJRklFRBAAEiMKH09QRU5JTkdfQ09OVkVSU0lPTl9TVEFURV9QT1NURUQQARIlCiFPUEVOSU5HX0NPTlZFUlNJT05fU1RBVEVfUkVQTEFDRUQQAiq5AgoST3BlbmluZ0JhbGFuY2VLaW5kEiQKIE9QRU5JTkdfQkFMQU5DRV9LSU5EX1VOU1BFQ0lGSUVEEAASIQodT1BFTklOR19CQUxBTkNFX0tJTkRfT1JESU5BUlkQARIrCidPUEVOSU5HX0JBTEFOQ0VfS0lORF9DVVNUT01FUl9PUEVOX0lURU0QAhIrCidPUEVOSU5HX0JBTEFOQ0VfS0lORF9TVVBQTElFUl9PUEVOX0lURU0QAxIqCiZPUEVOSU5HX0JBTEFOQ0VfS0lORF9GSU5BTkNJQUxfQUNDT1VOVBAEEisKJ09QRU5JTkdfQkFMQU5DRV9LSU5EX1VOQUxMT0NBVEVEX0NSRURJVBAFEicKI09QRU5JTkdfQkFMQU5DRV9LSU5EX09QRU5JTkdfRVFVSVRZEAYqYwoMSm91cm5hbFN0YXRlEh0KGUpPVVJOQUxfU1RBVEVfVU5TUEVDSUZJRUQQABIYChRKT1VSTkFMX1NUQVRFX1BPU1RFRBABEhoKFkpPVVJOQUxfU1RBVEVfUkVWRVJTRUQQAiqOAQoNSm91cm5hbFNvdXJjZRIeChpKT1VSTkFMX1NPVVJDRV9VTlNQRUNJRklFRBAAEiUKIUpPVVJOQUxfU09VUkNFX09QRU5JTkdfQ09OVkVSU0lPThABEhkKFUpPVVJOQUxfU09VUkNFX01BTlVBTBACEhsKF0pPVVJOQUxfU09VUkNFX1JFVkVSU0FMEAMqWwoLUGVyaW9kU3RhdGUSHAoYUEVSSU9EX1NUQVRFX1VOU1BFQ0lGSUVEEAASFQoRUEVSSU9EX1NUQVRFX09QRU4QARIXChNQRVJJT0RfU1RBVEVfQ0xPU0VEEAIqowEKDFRheFRyZWF0bWVudBIdChlUQVhfVFJFQVRNRU5UX1VOU1BFQ0lGSUVEEAASGQoVVEFYX1RSRUFUTUVOVF9UQVhBQkxFEAESGgoWVEFYX1RSRUFUTUVOVF9HU1RfRlJFRRACEh0KGVRBWF9UUkVBVE1FTlRfSU5QVVRfVEFYRUQQAxIeChpUQVhfVFJFQVRNRU5UX09VVF9PRl9TQ09QRRAEMu8KChFBY2NvdW50aW5nU2VydmljZRJQCg1DcmVhdGVBY2NvdW50Eh4udGFtbXkudjEuQ3JlYXRlQWNjb3VudFJlcXVlc3QaHy50YW1teS52MS5DcmVhdGVBY2NvdW50UmVzcG9uc2USUAoNVXBkYXRlQWNjb3VudBIeLnRhbW15LnYxLlVwZGF0ZUFjY291bnRSZXF1ZXN0Gh8udGFtbXkudjEuVXBkYXRlQWNjb3VudFJlc3BvbnNlElkKEFNldEFjY291bnRTdGF0dXMSIS50YW1teS52MS5TZXRBY2NvdW50U3RhdHVzUmVxdWVzdBoiLnRhbW15LnYxLlNldEFjY291bnRTdGF0dXNSZXNwb25zZRJNCgxMaXN0VGF4Q29kZXMSHS50YW1teS52MS5MaXN0VGF4Q29kZXNSZXF1ZXN0Gh4udGFtbXkudjEuTGlzdFRheENvZGVzUmVzcG9uc2USaAoVUG9zdE9wZW5pbmdDb252ZXJzaW9uEiYudGFtbXkudjEuUG9zdE9wZW5pbmdDb252ZXJzaW9uUmVxdWVzdBonLnRhbW15LnYxLlBvc3RPcGVuaW5nQ29udmVyc2lvblJlc3BvbnNlEnEKGFJlcGxhY2VPcGVuaW5nQ29udmVyc2lvbhIpLnRhbW15LnYxLlJlcGxhY2VPcGVuaW5nQ29udmVyc2lvblJlcXVlc3QaKi50YW1teS52MS5SZXBsYWNlT3BlbmluZ0NvbnZlcnNpb25SZXNwb25zZRJcChFQb3N0TWFudWFsSm91cm5hbBIiLnRhbW15LnYxLlBvc3RNYW51YWxKb3VybmFsUmVxdWVzdBojLnRhbW15LnYxLlBvc3RNYW51YWxKb3VybmFsUmVzcG9uc2USUwoOUmV2ZXJzZUpvdXJuYWwSHy50YW1teS52MS5SZXZlcnNlSm91cm5hbFJlcXVlc3QaIC50YW1teS52MS5SZXZlcnNlSm91cm5hbFJlc3BvbnNlEkoKC0Nsb3NlUGVyaW9kEhwudGFtbXkudjEuQ2xvc2VQZXJpb2RSZXF1ZXN0Gh0udGFtbXkudjEuQ2xvc2VQZXJpb2RSZXNwb25zZRJNCgxSZW9wZW5QZXJpb2QSHS50YW1teS52MS5SZW9wZW5QZXJpb2RSZXF1ZXN0Gh4udGFtbXkudjEuUmVvcGVuUGVyaW9kUmVzcG9uc2USRwoKR2V0QWNjb3VudBIbLnRhbW15LnYxLkdldEFjY291bnRSZXF1ZXN0GhwudGFtbXkudjEuR2V0QWNjb3VudFJlc3BvbnNlEk0KDExpc3RBY2NvdW50cxIdLnRhbW15LnYxLkxpc3RBY2NvdW50c1JlcXVlc3QaHi50YW1teS52MS5MaXN0QWNjb3VudHNSZXNwb25zZRJHCgpHZXRKb3VybmFsEhsudGFtbXkudjEuR2V0Sm91cm5hbFJlcXVlc3QaHC50YW1teS52MS5HZXRKb3VybmFsUmVzcG9uc2USTQoMTGlzdEpvdXJuYWxzEh0udGFtbXkudjEuTGlzdEpvdXJuYWxzUmVxdWVzdBoeLnRhbW15LnYxLkxpc3RKb3VybmFsc1Jlc3BvbnNlElkKEEdldEdlbmVyYWxMZWRnZXISIS50YW1teS52MS5HZXRHZW5lcmFsTGVkZ2VyUmVxdWVzdBoiLnRhbW15LnYxLkdldEdlbmVyYWxMZWRnZXJSZXNwb25zZRJWCg9HZXRUcmlhbEJhbGFuY2USIC50YW1teS52MS5HZXRUcmlhbEJhbGFuY2VSZXF1ZXN0GiEudGFtbXkudjEuR2V0VHJpYWxCYWxhbmNlUmVzcG9uc2VCR1pFZ2l0aHViLmNvbS90YW1teWFwcC90YW1teS9zZXJ2aWNlcy9jb3JlL2ludGVybmFsL2dlbi90YW1teS92MTt0YW1teXYxYgZwcm90bzM", [file_buf_validate_validate, file_google_protobuf_field_mask, file_google_protobuf_timestamp, file_tammy_v1_common]);

// packages/connect-client/src/gen/tammy/v1/invoices_pb.ts
var file_tammy_v1_invoices = /* @__PURE__ */ fileDesc("Chd0YW1teS92MS9pbnZvaWNlcy5wcm90bxIIdGFtbXkudjEigQMKEEludm9pY2VMaW5lSW5wdXQSHwoLZGVzY3JpcHRpb24YASABKAlCCrpIB3IFEAEY9AMSIwoOcXVhbnRpdHlfbWlsbGkYAiABKA1CC7pICCoGGMCEPSgBEiYKEHVuaXRfcHJpY2VfbWlub3IYAyABKANCDLpICSIHGIDC1y8oABIqCg91bml0X2Nvc3RfbWlub3IYBCABKANCDLpICSIHGIDC1y8oAEgAiAEBEjkKDXRheF90cmVhdG1lbnQYBSABKA4yFi50YW1teS52MS5UYXhUcmVhdG1lbnRCCrpIB4IBBBABIAASbQoSc291cmNlX2RvY3VtZW50X2lkGAYgASgJQky6SElyRzJFXlswLTlhLWZdezh9LVswLTlhLWZdezR9LTdbMC05YS1mXXszfS1bODlhYl1bMC05YS1mXXszfS1bMC05YS1mXXsxMn0kSAGIAQFCEgoQX3VuaXRfY29zdF9taW5vckIVChNfc291cmNlX2RvY3VtZW50X2lkIvUCCgxJbnZvaWNlRHJhZnQSFwoGbnVtYmVyGAEgASgJQge6SARyAhgoEhsKCmlzc3VlX2RhdGUYAiABKAlCB7pIBHICGAoSGQoIZHVlX2RhdGUYAyABKAlCB7pIBHICGAoSHwoNY3VzdG9tZXJfbmFtZRgEIAEoCUIIukgFcgMYyAESIgoQY3VzdG9tZXJfYWRkcmVzcxgFIAEoCUIIukgFcgMY6AcSHQoMY3VzdG9tZXJfYWJuGAYgASgJQge6SARyAhgLEiAKDnNlbGxlcl9hZGRyZXNzGAcgASgJQgi6SAVyAxjoBxIWCg5nc3RfcmVnaXN0ZXJlZBgIIAEoCBIXCgVub3RlcxgJIAEoCUIIukgFcgMY0A8SJgoUcGF5bWVudF9pbnN0cnVjdGlvbnMYCiABKAlCCLpIBXIDGOgHEjUKBWxpbmVzGAsgAygLMhoudGFtbXkudjEuSW52b2ljZUxpbmVJbnB1dEIKukgHkgEECAEQMiK6AQoQSW52b2ljZUxpbmVUb3RhbBImCg5zdWJ0b3RhbF9taW5vchgBIAEoA0IOukgLIgkYgKCUpY0dKAASIQoJZ3N0X21pbm9yGAIgASgDQg66SAsiCRiAoJSljR0oABIjCgt0b3RhbF9taW5vchgDIAEoA0IOukgLIgkYgKCUpY0dKAASJwoKY29zdF9taW5vchgEIAEoA0IOukgLIgkYgKCUpY0dKABIAIgBAUINCgtfY29zdF9taW5vciKCBgoHSW52b2ljZRJYCgJpZBgBIAEoCUJMukhJckcyRV5bMC05YS1mXXs4fS1bMC05YS1mXXs0fS03WzAtOWEtZl17M30tWzg5YWJdWzAtOWEtZl17M30tWzAtOWEtZl17MTJ9JBJlCg9vcmdhbmlzYXRpb25faWQYAiABKAlCTLpISXJHMkVeWzAtOWEtZl17OH0tWzAtOWEtZl17NH0tN1swLTlhLWZdezN9LVs4OWFiXVswLTlhLWZdezN9LVswLTlhLWZdezEyfSQSGAoHdmVyc2lvbhgDIAEoBEIHukgEMgIoARIxCgVzdGF0ZRgEIAEoDjIWLnRhbW15LnYxLkludm9pY2VTdGF0ZUIKukgHggEEEAEgABIfCgtzZWxsZXJfbmFtZRgFIAEoCUIKukgHcgUQARiAAhIcCgpzZWxsZXJfYWJuGAYgASgJQgi6SAVyA5gBCxItCgVkcmFmdBgHIAEoCzIWLnRhbW15LnYxLkludm9pY2VEcmFmdEIGukgDyAEBEjsKC2xpbmVfdG90YWxzGAggAygLMhoudGFtbXkudjEuSW52b2ljZUxpbmVUb3RhbEIKukgHkgEECAEQMhIWCg5zdWJ0b3RhbF9taW5vchgJIAEoAxIRCglnc3RfbWlub3IYCiABKAMSEwoLdG90YWxfbWlub3IYCyABKAMSEgoKY29zdF9taW5vchgMIAEoAxI2CgpjcmVhdGVkX2F0GA0gASgLMhouZ29vZ2xlLnByb3RvYnVmLlRpbWVzdGFtcEIGukgDyAEBEjYKCnVwZGF0ZWRfYXQYDiABKAsyGi5nb29nbGUucHJvdG9idWYuVGltZXN0YW1wQga6SAPIAQESaAoNbG9nb19hc3NldF9pZBgPIAEoCUJMukhJckcyRV5bMC05YS1mXXs4fS1bMC05YS1mXXs0fS03WzAtOWEtZl17M30tWzg5YWJdWzAtOWEtZl17M30tWzAtOWEtZl17MTJ9JEgAiAEBQhAKDl9sb2dvX2Fzc2V0X2lkItgCChdTYXZlSW52b2ljZURyYWZ0UmVxdWVzdBI5Cg9jb21tYW5kX2NvbnRleHQYASABKAsyGC50YW1teS52MS5Db21tYW5kQ29udGV4dEIGukgDyAEBEmUKD29yZ2FuaXNhdGlvbl9pZBgCIAEoCUJMukhJckcyRV5bMC05YS1mXXs4fS1bMC05YS1mXXs0fS03WzAtOWEtZl17M30tWzg5YWJdWzAtOWEtZl17M30tWzAtOWEtZl17MTJ9JBIbCgppbnZvaWNlX2lkGAMgASgJQge6SARyAhgkEhgKEGV4cGVjdGVkX3ZlcnNpb24YBCABKAQSLQoFZHJhZnQYBSABKAsyFi50YW1teS52MS5JbnZvaWNlRHJhZnRCBrpIA8gBARIjCg1sb2dvX2Fzc2V0X2lkGAYgASgJQge6SARyAhgkSACIAQFCEAoOX2xvZ29fYXNzZXRfaWQiRgoYU2F2ZUludm9pY2VEcmFmdFJlc3BvbnNlEioKB2ludm9pY2UYASABKAsyES50YW1teS52MS5JbnZvaWNlQga6SAPIAQEinQIKEUdldEludm9pY2VSZXF1ZXN0Ej8KDmF1dGhlbnRpY2F0aW9uGAEgASgLMh8udGFtbXkudjEuQXV0aGVudGljYXRpb25Db250ZXh0Qga6SAPIAQESZQoPb3JnYW5pc2F0aW9uX2lkGAIgASgJQky6SElyRzJFXlswLTlhLWZdezh9LVswLTlhLWZdezR9LTdbMC05YS1mXXszfS1bODlhYl1bMC05YS1mXXszfS1bMC05YS1mXXsxMn0kEmAKCmludm9pY2VfaWQYAyABKAlCTLpISXJHMkVeWzAtOWEtZl17OH0tWzAtOWEtZl17NH0tN1swLTlhLWZdezN9LVs4OWFiXVswLTlhLWZdezN9LVswLTlhLWZdezEyfSQiQAoSR2V0SW52b2ljZVJlc3BvbnNlEioKB2ludm9pY2UYASABKAsyES50YW1teS52MS5JbnZvaWNlQga6SAPIAQEi6gEKE0xpc3RJbnZvaWNlc1JlcXVlc3QSPwoOYXV0aGVudGljYXRpb24YASABKAsyHy50YW1teS52MS5BdXRoZW50aWNhdGlvbkNvbnRleHRCBrpIA8gBARJlCg9vcmdhbmlzYXRpb25faWQYAiABKAlCTLpISXJHMkVeWzAtOWEtZl17OH0tWzAtOWEtZl17NH0tN1swLTlhLWZdezN9LVs4OWFiXVswLTlhLWZdezN9LVswLTlhLWZdezEyfSQSKwoEcGFnZRgDIAEoCzIVLnRhbW15LnYxLlBhZ2VSZXF1ZXN0Qga6SAPIAQEicAoUTGlzdEludm9pY2VzUmVzcG9uc2USLgoIaW52b2ljZXMYASADKAsyES50YW1teS52MS5JbnZvaWNlQgm6SAaSAQMQyAESKAoEcGFnZRgCIAEoCzISLnRhbW15LnYxLlBhZ2VJbmZvQga6SAPIAQEivAIKE0lzc3VlSW52b2ljZVJlcXVlc3QSOQoPY29tbWFuZF9jb250ZXh0GAEgASgLMhgudGFtbXkudjEuQ29tbWFuZENvbnRleHRCBrpIA8gBARJlCg9vcmdhbmlzYXRpb25faWQYAiABKAlCTLpISXJHMkVeWzAtOWEtZl17OH0tWzAtOWEtZl17NH0tN1swLTlhLWZdezN9LVs4OWFiXVswLTlhLWZdezN9LVswLTlhLWZdezEyfSQSYAoKaW52b2ljZV9pZBgDIAEoCUJMukhJckcyRV5bMC05YS1mXXs4fS1bMC05YS1mXXs0fS03WzAtOWEtZl17M30tWzg5YWJdWzAtOWEtZl17M30tWzAtOWEtZl17MTJ9JBIhChBleHBlY3RlZF92ZXJzaW9uGAQgASgEQge6SAQyAigBIkIKFElzc3VlSW52b2ljZVJlc3BvbnNlEioKB2ludm9pY2UYASABKAsyES50YW1teS52MS5JbnZvaWNlQga6SAPIAQEimwIKFEludm9pY2VUZW1wbGF0ZUlucHV0EiAKDnNlbGxlcl9hZGRyZXNzGAEgASgJQgi6SAVyAxjoBxIXCgVub3RlcxgCIAEoCUIIukgFcgMY0A8SJgoUcGF5bWVudF9pbnN0cnVjdGlvbnMYAyABKAlCCLpIBXIDGOgHEiQKEnBheW1lbnRfdGVybXNfZGF5cxgEIAEoDUIIukgFKgMY7QISaAoNbG9nb19hc3NldF9pZBgFIAEoCUJMukhJckcyRV5bMC05YS1mXXs4fS1bMC05YS1mXXs0fS03WzAtOWEtZl17M30tWzg5YWJdWzAtOWEtZl17M30tWzAtOWEtZl17MTJ9JEgAiAEBQhAKDl9sb2dvX2Fzc2V0X2lkIsMBCg9JbnZvaWNlVGVtcGxhdGUSZQoPb3JnYW5pc2F0aW9uX2lkGAEgASgJQky6SElyRzJFXlswLTlhLWZdezh9LVswLTlhLWZdezR9LTdbMC05YS1mXXszfS1bODlhYl1bMC05YS1mXXszfS1bMC05YS1mXXsxMn0kEg8KB3ZlcnNpb24YAiABKAQSOAoIZGVmYXVsdHMYAyABKAsyHi50YW1teS52MS5JbnZvaWNlVGVtcGxhdGVJbnB1dEIGukgDyAEBIsMBChlHZXRJbnZvaWNlVGVtcGxhdGVSZXF1ZXN0Ej8KDmF1dGhlbnRpY2F0aW9uGAEgASgLMh8udGFtbXkudjEuQXV0aGVudGljYXRpb25Db250ZXh0Qga6SAPIAQESZQoPb3JnYW5pc2F0aW9uX2lkGAIgASgJQky6SElyRzJFXlswLTlhLWZdezh9LVswLTlhLWZdezR9LTdbMC05YS1mXXszfS1bODlhYl1bMC05YS1mXXszfS1bMC05YS1mXXsxMn0kIlEKGkdldEludm9pY2VUZW1wbGF0ZVJlc3BvbnNlEjMKCHRlbXBsYXRlGAEgASgLMhkudGFtbXkudjEuSW52b2ljZVRlbXBsYXRlQga6SAPIAQEikgIKGlNhdmVJbnZvaWNlVGVtcGxhdGVSZXF1ZXN0EjkKD2NvbW1hbmRfY29udGV4dBgBIAEoCzIYLnRhbW15LnYxLkNvbW1hbmRDb250ZXh0Qga6SAPIAQESZQoPb3JnYW5pc2F0aW9uX2lkGAIgASgJQky6SElyRzJFXlswLTlhLWZdezh9LVswLTlhLWZdezR9LTdbMC05YS1mXXszfS1bODlhYl1bMC05YS1mXXszfS1bMC05YS1mXXsxMn0kEhgKEGV4cGVjdGVkX3ZlcnNpb24YAyABKAQSOAoIZGVmYXVsdHMYBCABKAsyHi50YW1teS52MS5JbnZvaWNlVGVtcGxhdGVJbnB1dEIGukgDyAEBIlIKG1NhdmVJbnZvaWNlVGVtcGxhdGVSZXNwb25zZRIzCgh0ZW1wbGF0ZRgBIAEoCzIZLnRhbW15LnYxLkludm9pY2VUZW1wbGF0ZUIGukgDyAEBIqkDCgtJbnZvaWNlTG9nbxJYCgJpZBgBIAEoCUJMukhJckcyRV5bMC05YS1mXXs4fS1bMC05YS1mXXs0fS03WzAtOWEtZl17M30tWzg5YWJdWzAtOWEtZl17M30tWzAtOWEtZl17MTJ9JBJlCg9vcmdhbmlzYXRpb25faWQYAiABKAlCTLpISXJHMkVeWzAtOWEtZl17OH0tWzAtOWEtZl17NH0tN1swLTlhLWZdezN9LVs4OWFiXVswLTlhLWZdezN9LVswLTlhLWZdezEyfSQSLwoJbWltZV90eXBlGAMgASgJQhy6SBlyF1IJaW1hZ2UvcG5nUgppbWFnZS9qcGVnEiAKC2J5dGVfbGVuZ3RoGAQgASgNQgu6SAgqBhiAgEAoARIXCgZzaGEyNTYYBSABKAxCB7pIBHoCaCASGQoFd2lkdGgYBiABKA1CCrpIByoFGIAgKAESGgoGaGVpZ2h0GAcgASgNQgq6SAcqBRiAICgBEjYKCmNyZWF0ZWRfYXQYCCABKAsyGi5nb29nbGUucHJvdG9idWYuVGltZXN0YW1wQga6SAPIAQEiiwIKGFVwbG9hZEludm9pY2VMb2dvUmVxdWVzdBI5Cg9jb21tYW5kX2NvbnRleHQYASABKAsyGC50YW1teS52MS5Db21tYW5kQ29udGV4dEIGukgDyAEBEmUKD29yZ2FuaXNhdGlvbl9pZBgCIAEoCUJMukhJckcyRV5bMC05YS1mXXs4fS1bMC05YS1mXXs0fS03WzAtOWEtZl17M30tWzg5YWJdWzAtOWEtZl17M30tWzAtOWEtZl17MTJ9JBIvCgltaW1lX3R5cGUYAyABKAlCHLpIGXIXUglpbWFnZS9wbmdSCmltYWdlL2pwZWcSHAoHY29udGVudBgEIAEoDEILukgIegYQARiAgEAiSAoZVXBsb2FkSW52b2ljZUxvZ29SZXNwb25zZRIrCgRsb2dvGAEgASgLMhUudGFtbXkudjEuSW52b2ljZUxvZ29CBrpIA8gBASKkAgoVR2V0SW52b2ljZUxvZ29SZXF1ZXN0Ej8KDmF1dGhlbnRpY2F0aW9uGAEgASgLMh8udGFtbXkudjEuQXV0aGVudGljYXRpb25Db250ZXh0Qga6SAPIAQESZQoPb3JnYW5pc2F0aW9uX2lkGAIgASgJQky6SElyRzJFXlswLTlhLWZdezh9LVswLTlhLWZdezR9LTdbMC05YS1mXXszfS1bODlhYl1bMC05YS1mXXszfS1bMC05YS1mXXsxMn0kEmMKDWxvZ29fYXNzZXRfaWQYAyABKAlCTLpISXJHMkVeWzAtOWEtZl17OH0tWzAtOWEtZl17NH0tN1swLTlhLWZdezN9LVs4OWFiXVswLTlhLWZdezN9LVswLTlhLWZdezEyfSQiYwoWR2V0SW52b2ljZUxvZ29SZXNwb25zZRIrCgRsb2dvGAEgASgLMhUudGFtbXkudjEuSW52b2ljZUxvZ29CBrpIA8gBARIcCgdjb250ZW50GAIgASgMQgu6SAh6BhABGICAQCpgCgxJbnZvaWNlU3RhdGUSHQoZSU5WT0lDRV9TVEFURV9VTlNQRUNJRklFRBAAEhcKE0lOVk9JQ0VfU1RBVEVfRFJBRlQQARIYChRJTlZPSUNFX1NUQVRFX0lTU1VFRBACMsoFCg5JbnZvaWNlU2VydmljZRJZChBTYXZlSW52b2ljZURyYWZ0EiEudGFtbXkudjEuU2F2ZUludm9pY2VEcmFmdFJlcXVlc3QaIi50YW1teS52MS5TYXZlSW52b2ljZURyYWZ0UmVzcG9uc2USRwoKR2V0SW52b2ljZRIbLnRhbW15LnYxLkdldEludm9pY2VSZXF1ZXN0GhwudGFtbXkudjEuR2V0SW52b2ljZVJlc3BvbnNlEk0KDExpc3RJbnZvaWNlcxIdLnRhbW15LnYxLkxpc3RJbnZvaWNlc1JlcXVlc3QaHi50YW1teS52MS5MaXN0SW52b2ljZXNSZXNwb25zZRJNCgxJc3N1ZUludm9pY2USHS50YW1teS52MS5Jc3N1ZUludm9pY2VSZXF1ZXN0Gh4udGFtbXkudjEuSXNzdWVJbnZvaWNlUmVzcG9uc2USXwoSR2V0SW52b2ljZVRlbXBsYXRlEiMudGFtbXkudjEuR2V0SW52b2ljZVRlbXBsYXRlUmVxdWVzdBokLnRhbW15LnYxLkdldEludm9pY2VUZW1wbGF0ZVJlc3BvbnNlEmIKE1NhdmVJbnZvaWNlVGVtcGxhdGUSJC50YW1teS52MS5TYXZlSW52b2ljZVRlbXBsYXRlUmVxdWVzdBolLnRhbW15LnYxLlNhdmVJbnZvaWNlVGVtcGxhdGVSZXNwb25zZRJcChFVcGxvYWRJbnZvaWNlTG9nbxIiLnRhbW15LnYxLlVwbG9hZEludm9pY2VMb2dvUmVxdWVzdBojLnRhbW15LnYxLlVwbG9hZEludm9pY2VMb2dvUmVzcG9uc2USUwoOR2V0SW52b2ljZUxvZ28SHy50YW1teS52MS5HZXRJbnZvaWNlTG9nb1JlcXVlc3QaIC50YW1teS52MS5HZXRJbnZvaWNlTG9nb1Jlc3BvbnNlQkdaRWdpdGh1Yi5jb20vdGFtbXlhcHAvdGFtbXkvc2VydmljZXMvY29yZS9pbnRlcm5hbC9nZW4vdGFtbXkvdjE7dGFtbXl2MWIGcHJvdG8z", [file_buf_validate_validate, file_google_protobuf_timestamp, file_tammy_v1_accounting, file_tammy_v1_common]);
var InvoiceDraftSchema = /* @__PURE__ */ messageDesc(file_tammy_v1_invoices, 1);
var SaveInvoiceDraftRequestSchema = /* @__PURE__ */ messageDesc(file_tammy_v1_invoices, 4);
var SaveInvoiceDraftResponseSchema = /* @__PURE__ */ messageDesc(file_tammy_v1_invoices, 5);
var ListInvoicesRequestSchema = /* @__PURE__ */ messageDesc(file_tammy_v1_invoices, 8);
var ListInvoicesResponseSchema = /* @__PURE__ */ messageDesc(file_tammy_v1_invoices, 9);

// packages/invoices/src/model.ts
var InvoiceValidationError = class extends Error {
  constructor(message) {
    super(message);
    this.name = "InvoiceValidationError";
  }
};
var MAX_TOTAL = 1000000000000n;
var LINE_KEYS = /* @__PURE__ */ new Set([
  "description",
  "quantityMilli",
  "unitPriceMinor",
  "unitCostMinor",
  "taxTreatment",
  "sourceDocumentId"
]);
var DRAFT_KEYS = /* @__PURE__ */ new Set([
  "number",
  "issueDate",
  "dueDate",
  "customerName",
  "customerAddress",
  "customerAbn",
  "sellerAddress",
  "gstRegistered",
  "notes",
  "paymentInstructions",
  "lines"
]);
var TAX_TREATMENTS = /* @__PURE__ */ new Set([
  "taxable",
  "gst-free",
  "input-taxed",
  "out-of-scope"
]);
var DOCUMENT_ID = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;
var INVOICE_DRAFT_JSON_SCHEMA = {
  type: "object",
  additionalProperties: false,
  required: [
    "number",
    "issueDate",
    "dueDate",
    "customerName",
    "customerAddress",
    "sellerAddress",
    "gstRegistered",
    "notes",
    "paymentInstructions",
    "lines"
  ],
  properties: {
    number: { type: "string", maxLength: 40 },
    issueDate: {
      type: "string",
      pattern: "^(?:\\d{4}-\\d{2}-\\d{2})?$",
      description: "YYYY-MM-DD calendar date, or empty in an incomplete draft."
    },
    dueDate: {
      type: "string",
      pattern: "^(?:\\d{4}-\\d{2}-\\d{2})?$",
      description: "YYYY-MM-DD date not before issueDate, or empty in an incomplete draft."
    },
    customerName: { type: "string", maxLength: 200 },
    customerAddress: { type: "string", maxLength: 1e3 },
    customerAbn: { type: "string", maxLength: 20 },
    sellerAddress: { type: "string", maxLength: 1e3 },
    gstRegistered: {
      type: "boolean",
      description: "Seller GST-registration status; never inferred from an ABN."
    },
    notes: { type: "string", maxLength: 2e3 },
    paymentInstructions: { type: "string", maxLength: 1e3 },
    lines: {
      type: "array",
      minItems: 1,
      maxItems: 50,
      items: {
        type: "object",
        additionalProperties: false,
        required: ["description", "quantityMilli", "unitPriceMinor", "taxTreatment"],
        properties: {
          description: { type: "string", minLength: 1, maxLength: 500 },
          quantityMilli: {
            type: "integer",
            minimum: 1,
            maximum: 1e6,
            description: "Quantity in thousandths: 1500 means 1.5 units."
          },
          unitPriceMinor: {
            type: "integer",
            minimum: 0,
            maximum: 1e8,
            description: "Ex-GST selling price per unit, in AUD cents."
          },
          unitCostMinor: {
            type: "integer",
            minimum: 0,
            maximum: 1e8,
            description: "Private per-unit cost in AUD cents; excluded from customer PDFs."
          },
          taxTreatment: {
            type: "string",
            enum: ["taxable", "gst-free", "input-taxed", "out-of-scope"]
          },
          sourceDocumentId: {
            type: "string",
            pattern: DOCUMENT_ID.source,
            description: "Optional reviewed source document UUIDv7; application validates ownership."
          }
        }
      }
    }
  }
};
function invalid(field, reason) {
  throw new InvoiceValidationError(`${field}: ${reason}`);
}
function object(value, field, keys) {
  if (value === null || typeof value !== "object" || Array.isArray(value) || Object.getPrototypeOf(value) !== Object.prototype) {
    invalid(field, "must be an object");
  }
  for (const key of Reflect.ownKeys(value)) {
    if (typeof key !== "string" || !keys.has(key))
      invalid(`${field}.${String(key)}`, "is not supported");
  }
  return value;
}
function text(value, field, max, required = false) {
  if (typeof value !== "string" || !value.isWellFormed() || Array.from(value).length > max || Array.from(value).some((character) => {
    const code = character.charCodeAt(0);
    return code < 32 && code !== 9 && code !== 10 && code !== 13 || code === 127;
  })) {
    invalid(field, `must be text of at most ${max} characters without control characters`);
  }
  const normalized = value.trim();
  if (required && normalized.length === 0) invalid(field, "is required");
  return normalized;
}
function integer(value, field, min, max) {
  if (typeof value !== "number" || !Number.isSafeInteger(value) || value < min || value > max) {
    invalid(field, `must be an integer from ${min} to ${max}`);
  }
  return value;
}
function date(value, field) {
  if (value === "") return "";
  if (typeof value !== "string" || !/^\d{4}-\d{2}-\d{2}$/.test(value))
    invalid(field, "must be a YYYY-MM-DD date");
  const parsed = /* @__PURE__ */ new Date(`${value}T00:00:00.000Z`);
  if (!Number.isFinite(parsed.getTime()) || parsed.toISOString().slice(0, 10) !== value || value < "1900-01-01" || value > "9999-12-31") {
    invalid(field, "must be a valid calendar date from 1900 onwards");
  }
  return value;
}
function validateAbn(value, field = "abn", required = false) {
  const normalized = text(value, field, 20).replace(/ /g, "");
  if (normalized.length === 0 && !required) return "";
  if (!/^\d{11}$/.test(normalized)) invalid(field, "must contain a valid 11-digit ABN");
  const weights = [10, 1, 3, 5, 7, 9, 11, 13, 15, 17, 19];
  let checksum = 0;
  for (const [index, weight] of weights.entries()) {
    checksum += (Number(normalized[index]) - (index === 0 ? 1 : 0)) * weight;
  }
  if (checksum % 89 !== 0) invalid(field, "has an invalid ABN checksum");
  return normalized;
}
function line(value, index, gstRegistered) {
  const field = `lines[${index}]`;
  const input = object(value, field, LINE_KEYS);
  if (typeof input.taxTreatment !== "string" || !TAX_TREATMENTS.has(input.taxTreatment)) {
    invalid(`${field}.taxTreatment`, "must be taxable, gst-free, input-taxed or out-of-scope");
  }
  if (!gstRegistered && input.taxTreatment === "taxable")
    invalid("gstRegistered", "must be true to charge GST on taxable lines");
  const result = {
    description: text(input.description, `${field}.description`, 500, true),
    quantityMilli: integer(input.quantityMilli, `${field}.quantityMilli`, 1, 1e6),
    unitPriceMinor: integer(input.unitPriceMinor, `${field}.unitPriceMinor`, 0, 1e8),
    taxTreatment: input.taxTreatment
  };
  if (input.unitCostMinor !== void 0)
    result.unitCostMinor = integer(input.unitCostMinor, `${field}.unitCostMinor`, 0, 1e8);
  if (input.sourceDocumentId !== void 0) {
    const reference = text(input.sourceDocumentId, `${field}.sourceDocumentId`, 36);
    if (!DOCUMENT_ID.test(reference)) invalid(`${field}.sourceDocumentId`, "must be a UUIDv7");
    result.sourceDocumentId = reference.toLowerCase();
  }
  return result;
}
function validateInvoiceDraft(value) {
  const input = object(value, "invoice", DRAFT_KEYS);
  if (typeof input.gstRegistered !== "boolean") invalid("gstRegistered", "must be a boolean");
  if (!Array.isArray(input.lines) || input.lines.length < 1 || input.lines.length > 50)
    invalid("lines", "must contain 1 to 50 line items");
  const result = {
    number: text(input.number, "number", 40),
    issueDate: date(input.issueDate, "issueDate"),
    dueDate: date(input.dueDate, "dueDate"),
    customerName: text(input.customerName, "customerName", 200),
    customerAddress: text(input.customerAddress, "customerAddress", 1e3),
    sellerAddress: text(input.sellerAddress, "sellerAddress", 1e3),
    gstRegistered: input.gstRegistered,
    notes: text(input.notes, "notes", 2e3),
    paymentInstructions: text(input.paymentInstructions, "paymentInstructions", 1e3),
    lines: input.lines.map((value2, index) => line(value2, index, input.gstRegistered))
  };
  if (result.issueDate && result.dueDate && result.dueDate < result.issueDate)
    invalid("dueDate", "cannot precede issueDate");
  if (input.customerAbn !== void 0)
    result.customerAbn = validateAbn(input.customerAbn, "customerAbn");
  calculateInvoice(result);
  return result;
}
var halfUp = (numerator, divisor) => (numerator + divisor / 2n) / divisor;
function calculateInvoice(draft) {
  if (!Array.isArray(draft.lines) || draft.lines.length < 1 || draft.lines.length > 50)
    invalid("lines", "must contain 1 to 50 line items");
  let subtotal = 0n;
  let gst = 0n;
  let cost = 0n;
  const lines = draft.lines.map((item, index) => {
    const quantity = BigInt(
      integer(item.quantityMilli, `lines[${index}].quantityMilli`, 1, 1e6)
    );
    const price = BigInt(
      integer(item.unitPriceMinor, `lines[${index}].unitPriceMinor`, 0, 1e8)
    );
    const unitCost = BigInt(
      integer(item.unitCostMinor ?? 0, `lines[${index}].unitCostMinor`, 0, 1e8)
    );
    if (!TAX_TREATMENTS.has(item.taxTreatment))
      invalid(`lines[${index}].taxTreatment`, "is not supported");
    if (!draft.gstRegistered && item.taxTreatment === "taxable")
      invalid("gstRegistered", "must be true to charge GST on taxable lines");
    const netMinor = halfUp(quantity * price, 1000n);
    const gstMinor = item.taxTreatment === "taxable" ? halfUp(netMinor, 10n) : 0n;
    const costMinor = halfUp(quantity * unitCost, 1000n);
    subtotal += netMinor;
    gst += gstMinor;
    cost += costMinor;
    if (subtotal + gst > MAX_TOTAL || cost > MAX_TOTAL)
      invalid("total", "cannot exceed $10,000,000,000.00");
    return {
      netMinor: Number(netMinor),
      gstMinor: Number(gstMinor),
      totalMinor: Number(netMinor + gstMinor),
      costMinor: Number(costMinor)
    };
  });
  return {
    lines,
    subtotalMinor: Number(subtotal),
    gstMinor: Number(gst),
    totalMinor: Number(subtotal + gst),
    costMinor: Number(cost),
    marginMinor: Number(subtotal - cost)
  };
}
function validateInvoiceForIssue(draft, seller) {
  const validated = validateInvoiceDraft(draft);
  text(validated.number, "number", 40, true);
  text(validated.issueDate, "issueDate", 10, true);
  text(validated.dueDate, "dueDate", 10, true);
  text(validated.customerName, "customerName", 200, true);
  text(validated.customerAddress, "customerAddress", 1e3, true);
  text(validated.sellerAddress, "sellerAddress", 1e3, true);
  text(seller.name, "seller.name", 256, true);
  validateAbn(seller.abn, "seller.abn", true);
  return validated;
}
function formatAud(minor) {
  if (!Number.isSafeInteger(minor)) invalid("amount", "must be safe integer cents");
  const amount = Math.abs(minor);
  return `${minor < 0 ? "-" : ""}$${Math.floor(amount / 100).toLocaleString("en-AU")}.${String(amount % 100).padStart(2, "0")}`;
}
function formatQuantity(quantityMilli) {
  integer(quantityMilli, "quantityMilli", 1, 1e6);
  const decimal = String(quantityMilli % 1e3).padStart(3, "0").replace(/0+$/, "");
  return `${Math.floor(quantityMilli / 1e3)}${decimal.length ? `.${decimal}` : ""}`;
}

// apps/desktop/src/shared/invoices.ts
var taxValues = {
  taxable: 1 /* TAXABLE */,
  "gst-free": 2 /* GST_FREE */,
  "input-taxed": 3 /* INPUT_TAXED */,
  "out-of-scope": 4 /* OUT_OF_SCOPE */
};
function invoiceDraftToProto(value) {
  const draft = validateInvoiceDraft(value);
  return create(InvoiceDraftSchema, {
    ...draft,
    customerAbn: draft.customerAbn ?? "",
    lines: draft.lines.map((line2) => ({
      ...line2,
      unitPriceMinor: BigInt(line2.unitPriceMinor),
      unitCostMinor: line2.unitCostMinor === void 0 ? void 0 : BigInt(line2.unitCostMinor),
      taxTreatment: taxValues[line2.taxTreatment]
    }))
  });
}

// packages/invoices/src/pdf.ts
import { renderDocument } from "/Users/benebsworth/projects/tammy/.worktrees/issue/sbr-registration-ready/node_modules/.pnpm/@formepdf+core@0.25.0_react@19.2.7/node_modules/@formepdf/core/dist/index.js";

// node_modules/.pnpm/@formepdf+react@0.25.0_react@19.2.7/node_modules/@formepdf/react/dist/components.js
function Document(_props) {
  return null;
}
Document.__formeType = "Document";
function Page(_props) {
  return null;
}
function View(_props) {
  return null;
}
function Text(_props) {
  return null;
}
function Image(_props) {
  return null;
}
function Table(_props) {
  return null;
}
function Row(_props) {
  return null;
}
function Cell(_props) {
  return null;
}
function Fixed(_props) {
  return null;
}
function Watermark(_props) {
  return null;
}

// node_modules/.pnpm/@formepdf+react@0.25.0_react@19.2.7/node_modules/@formepdf/react/dist/charts.js
var import_react = __toESM(require_react(), 1);

// node_modules/.pnpm/@formepdf+react@0.25.0_react@19.2.7/node_modules/@formepdf/react/dist/serialize.js
var import_react2 = __toESM(require_react(), 1);

// node_modules/.pnpm/parse5@7.3.0/node_modules/parse5/dist/common/unicode.js
var CODE_POINTS;
(function(CODE_POINTS2) {
  CODE_POINTS2[CODE_POINTS2["EOF"] = -1] = "EOF";
  CODE_POINTS2[CODE_POINTS2["NULL"] = 0] = "NULL";
  CODE_POINTS2[CODE_POINTS2["TABULATION"] = 9] = "TABULATION";
  CODE_POINTS2[CODE_POINTS2["CARRIAGE_RETURN"] = 13] = "CARRIAGE_RETURN";
  CODE_POINTS2[CODE_POINTS2["LINE_FEED"] = 10] = "LINE_FEED";
  CODE_POINTS2[CODE_POINTS2["FORM_FEED"] = 12] = "FORM_FEED";
  CODE_POINTS2[CODE_POINTS2["SPACE"] = 32] = "SPACE";
  CODE_POINTS2[CODE_POINTS2["EXCLAMATION_MARK"] = 33] = "EXCLAMATION_MARK";
  CODE_POINTS2[CODE_POINTS2["QUOTATION_MARK"] = 34] = "QUOTATION_MARK";
  CODE_POINTS2[CODE_POINTS2["AMPERSAND"] = 38] = "AMPERSAND";
  CODE_POINTS2[CODE_POINTS2["APOSTROPHE"] = 39] = "APOSTROPHE";
  CODE_POINTS2[CODE_POINTS2["HYPHEN_MINUS"] = 45] = "HYPHEN_MINUS";
  CODE_POINTS2[CODE_POINTS2["SOLIDUS"] = 47] = "SOLIDUS";
  CODE_POINTS2[CODE_POINTS2["DIGIT_0"] = 48] = "DIGIT_0";
  CODE_POINTS2[CODE_POINTS2["DIGIT_9"] = 57] = "DIGIT_9";
  CODE_POINTS2[CODE_POINTS2["SEMICOLON"] = 59] = "SEMICOLON";
  CODE_POINTS2[CODE_POINTS2["LESS_THAN_SIGN"] = 60] = "LESS_THAN_SIGN";
  CODE_POINTS2[CODE_POINTS2["EQUALS_SIGN"] = 61] = "EQUALS_SIGN";
  CODE_POINTS2[CODE_POINTS2["GREATER_THAN_SIGN"] = 62] = "GREATER_THAN_SIGN";
  CODE_POINTS2[CODE_POINTS2["QUESTION_MARK"] = 63] = "QUESTION_MARK";
  CODE_POINTS2[CODE_POINTS2["LATIN_CAPITAL_A"] = 65] = "LATIN_CAPITAL_A";
  CODE_POINTS2[CODE_POINTS2["LATIN_CAPITAL_Z"] = 90] = "LATIN_CAPITAL_Z";
  CODE_POINTS2[CODE_POINTS2["RIGHT_SQUARE_BRACKET"] = 93] = "RIGHT_SQUARE_BRACKET";
  CODE_POINTS2[CODE_POINTS2["GRAVE_ACCENT"] = 96] = "GRAVE_ACCENT";
  CODE_POINTS2[CODE_POINTS2["LATIN_SMALL_A"] = 97] = "LATIN_SMALL_A";
  CODE_POINTS2[CODE_POINTS2["LATIN_SMALL_Z"] = 122] = "LATIN_SMALL_Z";
})(CODE_POINTS || (CODE_POINTS = {}));

// node_modules/.pnpm/parse5@7.3.0/node_modules/parse5/dist/common/error-codes.js
var ERR;
(function(ERR2) {
  ERR2["controlCharacterInInputStream"] = "control-character-in-input-stream";
  ERR2["noncharacterInInputStream"] = "noncharacter-in-input-stream";
  ERR2["surrogateInInputStream"] = "surrogate-in-input-stream";
  ERR2["nonVoidHtmlElementStartTagWithTrailingSolidus"] = "non-void-html-element-start-tag-with-trailing-solidus";
  ERR2["endTagWithAttributes"] = "end-tag-with-attributes";
  ERR2["endTagWithTrailingSolidus"] = "end-tag-with-trailing-solidus";
  ERR2["unexpectedSolidusInTag"] = "unexpected-solidus-in-tag";
  ERR2["unexpectedNullCharacter"] = "unexpected-null-character";
  ERR2["unexpectedQuestionMarkInsteadOfTagName"] = "unexpected-question-mark-instead-of-tag-name";
  ERR2["invalidFirstCharacterOfTagName"] = "invalid-first-character-of-tag-name";
  ERR2["unexpectedEqualsSignBeforeAttributeName"] = "unexpected-equals-sign-before-attribute-name";
  ERR2["missingEndTagName"] = "missing-end-tag-name";
  ERR2["unexpectedCharacterInAttributeName"] = "unexpected-character-in-attribute-name";
  ERR2["unknownNamedCharacterReference"] = "unknown-named-character-reference";
  ERR2["missingSemicolonAfterCharacterReference"] = "missing-semicolon-after-character-reference";
  ERR2["unexpectedCharacterAfterDoctypeSystemIdentifier"] = "unexpected-character-after-doctype-system-identifier";
  ERR2["unexpectedCharacterInUnquotedAttributeValue"] = "unexpected-character-in-unquoted-attribute-value";
  ERR2["eofBeforeTagName"] = "eof-before-tag-name";
  ERR2["eofInTag"] = "eof-in-tag";
  ERR2["missingAttributeValue"] = "missing-attribute-value";
  ERR2["missingWhitespaceBetweenAttributes"] = "missing-whitespace-between-attributes";
  ERR2["missingWhitespaceAfterDoctypePublicKeyword"] = "missing-whitespace-after-doctype-public-keyword";
  ERR2["missingWhitespaceBetweenDoctypePublicAndSystemIdentifiers"] = "missing-whitespace-between-doctype-public-and-system-identifiers";
  ERR2["missingWhitespaceAfterDoctypeSystemKeyword"] = "missing-whitespace-after-doctype-system-keyword";
  ERR2["missingQuoteBeforeDoctypePublicIdentifier"] = "missing-quote-before-doctype-public-identifier";
  ERR2["missingQuoteBeforeDoctypeSystemIdentifier"] = "missing-quote-before-doctype-system-identifier";
  ERR2["missingDoctypePublicIdentifier"] = "missing-doctype-public-identifier";
  ERR2["missingDoctypeSystemIdentifier"] = "missing-doctype-system-identifier";
  ERR2["abruptDoctypePublicIdentifier"] = "abrupt-doctype-public-identifier";
  ERR2["abruptDoctypeSystemIdentifier"] = "abrupt-doctype-system-identifier";
  ERR2["cdataInHtmlContent"] = "cdata-in-html-content";
  ERR2["incorrectlyOpenedComment"] = "incorrectly-opened-comment";
  ERR2["eofInScriptHtmlCommentLikeText"] = "eof-in-script-html-comment-like-text";
  ERR2["eofInDoctype"] = "eof-in-doctype";
  ERR2["nestedComment"] = "nested-comment";
  ERR2["abruptClosingOfEmptyComment"] = "abrupt-closing-of-empty-comment";
  ERR2["eofInComment"] = "eof-in-comment";
  ERR2["incorrectlyClosedComment"] = "incorrectly-closed-comment";
  ERR2["eofInCdata"] = "eof-in-cdata";
  ERR2["absenceOfDigitsInNumericCharacterReference"] = "absence-of-digits-in-numeric-character-reference";
  ERR2["nullCharacterReference"] = "null-character-reference";
  ERR2["surrogateCharacterReference"] = "surrogate-character-reference";
  ERR2["characterReferenceOutsideUnicodeRange"] = "character-reference-outside-unicode-range";
  ERR2["controlCharacterReference"] = "control-character-reference";
  ERR2["noncharacterCharacterReference"] = "noncharacter-character-reference";
  ERR2["missingWhitespaceBeforeDoctypeName"] = "missing-whitespace-before-doctype-name";
  ERR2["missingDoctypeName"] = "missing-doctype-name";
  ERR2["invalidCharacterSequenceAfterDoctypeName"] = "invalid-character-sequence-after-doctype-name";
  ERR2["duplicateAttribute"] = "duplicate-attribute";
  ERR2["nonConformingDoctype"] = "non-conforming-doctype";
  ERR2["missingDoctype"] = "missing-doctype";
  ERR2["misplacedDoctype"] = "misplaced-doctype";
  ERR2["endTagWithoutMatchingOpenElement"] = "end-tag-without-matching-open-element";
  ERR2["closingOfElementWithOpenChildElements"] = "closing-of-element-with-open-child-elements";
  ERR2["disallowedContentInNoscriptInHead"] = "disallowed-content-in-noscript-in-head";
  ERR2["openElementsLeftAfterEof"] = "open-elements-left-after-eof";
  ERR2["abandonedHeadElementChild"] = "abandoned-head-element-child";
  ERR2["misplacedStartTagForHeadElement"] = "misplaced-start-tag-for-head-element";
  ERR2["nestedNoscriptInHead"] = "nested-noscript-in-head";
  ERR2["eofInElementThatCanContainOnlyText"] = "eof-in-element-that-can-contain-only-text";
})(ERR || (ERR = {}));

// node_modules/.pnpm/parse5@7.3.0/node_modules/parse5/dist/tokenizer/preprocessor.js
var DEFAULT_BUFFER_WATERLINE = 1 << 16;

// node_modules/.pnpm/parse5@7.3.0/node_modules/parse5/dist/common/token.js
var TokenType;
(function(TokenType2) {
  TokenType2[TokenType2["CHARACTER"] = 0] = "CHARACTER";
  TokenType2[TokenType2["NULL_CHARACTER"] = 1] = "NULL_CHARACTER";
  TokenType2[TokenType2["WHITESPACE_CHARACTER"] = 2] = "WHITESPACE_CHARACTER";
  TokenType2[TokenType2["START_TAG"] = 3] = "START_TAG";
  TokenType2[TokenType2["END_TAG"] = 4] = "END_TAG";
  TokenType2[TokenType2["COMMENT"] = 5] = "COMMENT";
  TokenType2[TokenType2["DOCTYPE"] = 6] = "DOCTYPE";
  TokenType2[TokenType2["EOF"] = 7] = "EOF";
  TokenType2[TokenType2["HIBERNATION"] = 8] = "HIBERNATION";
})(TokenType || (TokenType = {}));

// node_modules/.pnpm/entities@6.0.1/node_modules/entities/dist/esm/decode-codepoint.js
var _a;
var fromCodePoint = (
  // eslint-disable-next-line @typescript-eslint/no-unnecessary-condition, n/no-unsupported-features/es-builtins
  (_a = String.fromCodePoint) !== null && _a !== void 0 ? _a : function(codePoint) {
    let output = "";
    if (codePoint > 65535) {
      codePoint -= 65536;
      output += String.fromCharCode(codePoint >>> 10 & 1023 | 55296);
      codePoint = 56320 | codePoint & 1023;
    }
    output += String.fromCharCode(codePoint);
    return output;
  }
);

// node_modules/.pnpm/entities@6.0.1/node_modules/entities/dist/esm/decode.js
var CharCodes;
(function(CharCodes2) {
  CharCodes2[CharCodes2["NUM"] = 35] = "NUM";
  CharCodes2[CharCodes2["SEMI"] = 59] = "SEMI";
  CharCodes2[CharCodes2["EQUALS"] = 61] = "EQUALS";
  CharCodes2[CharCodes2["ZERO"] = 48] = "ZERO";
  CharCodes2[CharCodes2["NINE"] = 57] = "NINE";
  CharCodes2[CharCodes2["LOWER_A"] = 97] = "LOWER_A";
  CharCodes2[CharCodes2["LOWER_F"] = 102] = "LOWER_F";
  CharCodes2[CharCodes2["LOWER_X"] = 120] = "LOWER_X";
  CharCodes2[CharCodes2["LOWER_Z"] = 122] = "LOWER_Z";
  CharCodes2[CharCodes2["UPPER_A"] = 65] = "UPPER_A";
  CharCodes2[CharCodes2["UPPER_F"] = 70] = "UPPER_F";
  CharCodes2[CharCodes2["UPPER_Z"] = 90] = "UPPER_Z";
})(CharCodes || (CharCodes = {}));
var BinTrieFlags;
(function(BinTrieFlags2) {
  BinTrieFlags2[BinTrieFlags2["VALUE_LENGTH"] = 49152] = "VALUE_LENGTH";
  BinTrieFlags2[BinTrieFlags2["BRANCH_LENGTH"] = 16256] = "BRANCH_LENGTH";
  BinTrieFlags2[BinTrieFlags2["JUMP_TABLE"] = 127] = "JUMP_TABLE";
})(BinTrieFlags || (BinTrieFlags = {}));
var EntityDecoderState;
(function(EntityDecoderState2) {
  EntityDecoderState2[EntityDecoderState2["EntityStart"] = 0] = "EntityStart";
  EntityDecoderState2[EntityDecoderState2["NumericStart"] = 1] = "NumericStart";
  EntityDecoderState2[EntityDecoderState2["NumericDecimal"] = 2] = "NumericDecimal";
  EntityDecoderState2[EntityDecoderState2["NumericHex"] = 3] = "NumericHex";
  EntityDecoderState2[EntityDecoderState2["NamedEntity"] = 4] = "NamedEntity";
})(EntityDecoderState || (EntityDecoderState = {}));
var DecodingMode;
(function(DecodingMode2) {
  DecodingMode2[DecodingMode2["Legacy"] = 0] = "Legacy";
  DecodingMode2[DecodingMode2["Strict"] = 1] = "Strict";
  DecodingMode2[DecodingMode2["Attribute"] = 2] = "Attribute";
})(DecodingMode || (DecodingMode = {}));

// node_modules/.pnpm/parse5@7.3.0/node_modules/parse5/dist/common/html.js
var NS;
(function(NS2) {
  NS2["HTML"] = "http://www.w3.org/1999/xhtml";
  NS2["MATHML"] = "http://www.w3.org/1998/Math/MathML";
  NS2["SVG"] = "http://www.w3.org/2000/svg";
  NS2["XLINK"] = "http://www.w3.org/1999/xlink";
  NS2["XML"] = "http://www.w3.org/XML/1998/namespace";
  NS2["XMLNS"] = "http://www.w3.org/2000/xmlns/";
})(NS || (NS = {}));
var ATTRS;
(function(ATTRS2) {
  ATTRS2["TYPE"] = "type";
  ATTRS2["ACTION"] = "action";
  ATTRS2["ENCODING"] = "encoding";
  ATTRS2["PROMPT"] = "prompt";
  ATTRS2["NAME"] = "name";
  ATTRS2["COLOR"] = "color";
  ATTRS2["FACE"] = "face";
  ATTRS2["SIZE"] = "size";
})(ATTRS || (ATTRS = {}));
var DOCUMENT_MODE;
(function(DOCUMENT_MODE2) {
  DOCUMENT_MODE2["NO_QUIRKS"] = "no-quirks";
  DOCUMENT_MODE2["QUIRKS"] = "quirks";
  DOCUMENT_MODE2["LIMITED_QUIRKS"] = "limited-quirks";
})(DOCUMENT_MODE || (DOCUMENT_MODE = {}));
var TAG_NAMES;
(function(TAG_NAMES2) {
  TAG_NAMES2["A"] = "a";
  TAG_NAMES2["ADDRESS"] = "address";
  TAG_NAMES2["ANNOTATION_XML"] = "annotation-xml";
  TAG_NAMES2["APPLET"] = "applet";
  TAG_NAMES2["AREA"] = "area";
  TAG_NAMES2["ARTICLE"] = "article";
  TAG_NAMES2["ASIDE"] = "aside";
  TAG_NAMES2["B"] = "b";
  TAG_NAMES2["BASE"] = "base";
  TAG_NAMES2["BASEFONT"] = "basefont";
  TAG_NAMES2["BGSOUND"] = "bgsound";
  TAG_NAMES2["BIG"] = "big";
  TAG_NAMES2["BLOCKQUOTE"] = "blockquote";
  TAG_NAMES2["BODY"] = "body";
  TAG_NAMES2["BR"] = "br";
  TAG_NAMES2["BUTTON"] = "button";
  TAG_NAMES2["CAPTION"] = "caption";
  TAG_NAMES2["CENTER"] = "center";
  TAG_NAMES2["CODE"] = "code";
  TAG_NAMES2["COL"] = "col";
  TAG_NAMES2["COLGROUP"] = "colgroup";
  TAG_NAMES2["DD"] = "dd";
  TAG_NAMES2["DESC"] = "desc";
  TAG_NAMES2["DETAILS"] = "details";
  TAG_NAMES2["DIALOG"] = "dialog";
  TAG_NAMES2["DIR"] = "dir";
  TAG_NAMES2["DIV"] = "div";
  TAG_NAMES2["DL"] = "dl";
  TAG_NAMES2["DT"] = "dt";
  TAG_NAMES2["EM"] = "em";
  TAG_NAMES2["EMBED"] = "embed";
  TAG_NAMES2["FIELDSET"] = "fieldset";
  TAG_NAMES2["FIGCAPTION"] = "figcaption";
  TAG_NAMES2["FIGURE"] = "figure";
  TAG_NAMES2["FONT"] = "font";
  TAG_NAMES2["FOOTER"] = "footer";
  TAG_NAMES2["FOREIGN_OBJECT"] = "foreignObject";
  TAG_NAMES2["FORM"] = "form";
  TAG_NAMES2["FRAME"] = "frame";
  TAG_NAMES2["FRAMESET"] = "frameset";
  TAG_NAMES2["H1"] = "h1";
  TAG_NAMES2["H2"] = "h2";
  TAG_NAMES2["H3"] = "h3";
  TAG_NAMES2["H4"] = "h4";
  TAG_NAMES2["H5"] = "h5";
  TAG_NAMES2["H6"] = "h6";
  TAG_NAMES2["HEAD"] = "head";
  TAG_NAMES2["HEADER"] = "header";
  TAG_NAMES2["HGROUP"] = "hgroup";
  TAG_NAMES2["HR"] = "hr";
  TAG_NAMES2["HTML"] = "html";
  TAG_NAMES2["I"] = "i";
  TAG_NAMES2["IMG"] = "img";
  TAG_NAMES2["IMAGE"] = "image";
  TAG_NAMES2["INPUT"] = "input";
  TAG_NAMES2["IFRAME"] = "iframe";
  TAG_NAMES2["KEYGEN"] = "keygen";
  TAG_NAMES2["LABEL"] = "label";
  TAG_NAMES2["LI"] = "li";
  TAG_NAMES2["LINK"] = "link";
  TAG_NAMES2["LISTING"] = "listing";
  TAG_NAMES2["MAIN"] = "main";
  TAG_NAMES2["MALIGNMARK"] = "malignmark";
  TAG_NAMES2["MARQUEE"] = "marquee";
  TAG_NAMES2["MATH"] = "math";
  TAG_NAMES2["MENU"] = "menu";
  TAG_NAMES2["META"] = "meta";
  TAG_NAMES2["MGLYPH"] = "mglyph";
  TAG_NAMES2["MI"] = "mi";
  TAG_NAMES2["MO"] = "mo";
  TAG_NAMES2["MN"] = "mn";
  TAG_NAMES2["MS"] = "ms";
  TAG_NAMES2["MTEXT"] = "mtext";
  TAG_NAMES2["NAV"] = "nav";
  TAG_NAMES2["NOBR"] = "nobr";
  TAG_NAMES2["NOFRAMES"] = "noframes";
  TAG_NAMES2["NOEMBED"] = "noembed";
  TAG_NAMES2["NOSCRIPT"] = "noscript";
  TAG_NAMES2["OBJECT"] = "object";
  TAG_NAMES2["OL"] = "ol";
  TAG_NAMES2["OPTGROUP"] = "optgroup";
  TAG_NAMES2["OPTION"] = "option";
  TAG_NAMES2["P"] = "p";
  TAG_NAMES2["PARAM"] = "param";
  TAG_NAMES2["PLAINTEXT"] = "plaintext";
  TAG_NAMES2["PRE"] = "pre";
  TAG_NAMES2["RB"] = "rb";
  TAG_NAMES2["RP"] = "rp";
  TAG_NAMES2["RT"] = "rt";
  TAG_NAMES2["RTC"] = "rtc";
  TAG_NAMES2["RUBY"] = "ruby";
  TAG_NAMES2["S"] = "s";
  TAG_NAMES2["SCRIPT"] = "script";
  TAG_NAMES2["SEARCH"] = "search";
  TAG_NAMES2["SECTION"] = "section";
  TAG_NAMES2["SELECT"] = "select";
  TAG_NAMES2["SOURCE"] = "source";
  TAG_NAMES2["SMALL"] = "small";
  TAG_NAMES2["SPAN"] = "span";
  TAG_NAMES2["STRIKE"] = "strike";
  TAG_NAMES2["STRONG"] = "strong";
  TAG_NAMES2["STYLE"] = "style";
  TAG_NAMES2["SUB"] = "sub";
  TAG_NAMES2["SUMMARY"] = "summary";
  TAG_NAMES2["SUP"] = "sup";
  TAG_NAMES2["TABLE"] = "table";
  TAG_NAMES2["TBODY"] = "tbody";
  TAG_NAMES2["TEMPLATE"] = "template";
  TAG_NAMES2["TEXTAREA"] = "textarea";
  TAG_NAMES2["TFOOT"] = "tfoot";
  TAG_NAMES2["TD"] = "td";
  TAG_NAMES2["TH"] = "th";
  TAG_NAMES2["THEAD"] = "thead";
  TAG_NAMES2["TITLE"] = "title";
  TAG_NAMES2["TR"] = "tr";
  TAG_NAMES2["TRACK"] = "track";
  TAG_NAMES2["TT"] = "tt";
  TAG_NAMES2["U"] = "u";
  TAG_NAMES2["UL"] = "ul";
  TAG_NAMES2["SVG"] = "svg";
  TAG_NAMES2["VAR"] = "var";
  TAG_NAMES2["WBR"] = "wbr";
  TAG_NAMES2["XMP"] = "xmp";
})(TAG_NAMES || (TAG_NAMES = {}));
var TAG_ID;
(function(TAG_ID2) {
  TAG_ID2[TAG_ID2["UNKNOWN"] = 0] = "UNKNOWN";
  TAG_ID2[TAG_ID2["A"] = 1] = "A";
  TAG_ID2[TAG_ID2["ADDRESS"] = 2] = "ADDRESS";
  TAG_ID2[TAG_ID2["ANNOTATION_XML"] = 3] = "ANNOTATION_XML";
  TAG_ID2[TAG_ID2["APPLET"] = 4] = "APPLET";
  TAG_ID2[TAG_ID2["AREA"] = 5] = "AREA";
  TAG_ID2[TAG_ID2["ARTICLE"] = 6] = "ARTICLE";
  TAG_ID2[TAG_ID2["ASIDE"] = 7] = "ASIDE";
  TAG_ID2[TAG_ID2["B"] = 8] = "B";
  TAG_ID2[TAG_ID2["BASE"] = 9] = "BASE";
  TAG_ID2[TAG_ID2["BASEFONT"] = 10] = "BASEFONT";
  TAG_ID2[TAG_ID2["BGSOUND"] = 11] = "BGSOUND";
  TAG_ID2[TAG_ID2["BIG"] = 12] = "BIG";
  TAG_ID2[TAG_ID2["BLOCKQUOTE"] = 13] = "BLOCKQUOTE";
  TAG_ID2[TAG_ID2["BODY"] = 14] = "BODY";
  TAG_ID2[TAG_ID2["BR"] = 15] = "BR";
  TAG_ID2[TAG_ID2["BUTTON"] = 16] = "BUTTON";
  TAG_ID2[TAG_ID2["CAPTION"] = 17] = "CAPTION";
  TAG_ID2[TAG_ID2["CENTER"] = 18] = "CENTER";
  TAG_ID2[TAG_ID2["CODE"] = 19] = "CODE";
  TAG_ID2[TAG_ID2["COL"] = 20] = "COL";
  TAG_ID2[TAG_ID2["COLGROUP"] = 21] = "COLGROUP";
  TAG_ID2[TAG_ID2["DD"] = 22] = "DD";
  TAG_ID2[TAG_ID2["DESC"] = 23] = "DESC";
  TAG_ID2[TAG_ID2["DETAILS"] = 24] = "DETAILS";
  TAG_ID2[TAG_ID2["DIALOG"] = 25] = "DIALOG";
  TAG_ID2[TAG_ID2["DIR"] = 26] = "DIR";
  TAG_ID2[TAG_ID2["DIV"] = 27] = "DIV";
  TAG_ID2[TAG_ID2["DL"] = 28] = "DL";
  TAG_ID2[TAG_ID2["DT"] = 29] = "DT";
  TAG_ID2[TAG_ID2["EM"] = 30] = "EM";
  TAG_ID2[TAG_ID2["EMBED"] = 31] = "EMBED";
  TAG_ID2[TAG_ID2["FIELDSET"] = 32] = "FIELDSET";
  TAG_ID2[TAG_ID2["FIGCAPTION"] = 33] = "FIGCAPTION";
  TAG_ID2[TAG_ID2["FIGURE"] = 34] = "FIGURE";
  TAG_ID2[TAG_ID2["FONT"] = 35] = "FONT";
  TAG_ID2[TAG_ID2["FOOTER"] = 36] = "FOOTER";
  TAG_ID2[TAG_ID2["FOREIGN_OBJECT"] = 37] = "FOREIGN_OBJECT";
  TAG_ID2[TAG_ID2["FORM"] = 38] = "FORM";
  TAG_ID2[TAG_ID2["FRAME"] = 39] = "FRAME";
  TAG_ID2[TAG_ID2["FRAMESET"] = 40] = "FRAMESET";
  TAG_ID2[TAG_ID2["H1"] = 41] = "H1";
  TAG_ID2[TAG_ID2["H2"] = 42] = "H2";
  TAG_ID2[TAG_ID2["H3"] = 43] = "H3";
  TAG_ID2[TAG_ID2["H4"] = 44] = "H4";
  TAG_ID2[TAG_ID2["H5"] = 45] = "H5";
  TAG_ID2[TAG_ID2["H6"] = 46] = "H6";
  TAG_ID2[TAG_ID2["HEAD"] = 47] = "HEAD";
  TAG_ID2[TAG_ID2["HEADER"] = 48] = "HEADER";
  TAG_ID2[TAG_ID2["HGROUP"] = 49] = "HGROUP";
  TAG_ID2[TAG_ID2["HR"] = 50] = "HR";
  TAG_ID2[TAG_ID2["HTML"] = 51] = "HTML";
  TAG_ID2[TAG_ID2["I"] = 52] = "I";
  TAG_ID2[TAG_ID2["IMG"] = 53] = "IMG";
  TAG_ID2[TAG_ID2["IMAGE"] = 54] = "IMAGE";
  TAG_ID2[TAG_ID2["INPUT"] = 55] = "INPUT";
  TAG_ID2[TAG_ID2["IFRAME"] = 56] = "IFRAME";
  TAG_ID2[TAG_ID2["KEYGEN"] = 57] = "KEYGEN";
  TAG_ID2[TAG_ID2["LABEL"] = 58] = "LABEL";
  TAG_ID2[TAG_ID2["LI"] = 59] = "LI";
  TAG_ID2[TAG_ID2["LINK"] = 60] = "LINK";
  TAG_ID2[TAG_ID2["LISTING"] = 61] = "LISTING";
  TAG_ID2[TAG_ID2["MAIN"] = 62] = "MAIN";
  TAG_ID2[TAG_ID2["MALIGNMARK"] = 63] = "MALIGNMARK";
  TAG_ID2[TAG_ID2["MARQUEE"] = 64] = "MARQUEE";
  TAG_ID2[TAG_ID2["MATH"] = 65] = "MATH";
  TAG_ID2[TAG_ID2["MENU"] = 66] = "MENU";
  TAG_ID2[TAG_ID2["META"] = 67] = "META";
  TAG_ID2[TAG_ID2["MGLYPH"] = 68] = "MGLYPH";
  TAG_ID2[TAG_ID2["MI"] = 69] = "MI";
  TAG_ID2[TAG_ID2["MO"] = 70] = "MO";
  TAG_ID2[TAG_ID2["MN"] = 71] = "MN";
  TAG_ID2[TAG_ID2["MS"] = 72] = "MS";
  TAG_ID2[TAG_ID2["MTEXT"] = 73] = "MTEXT";
  TAG_ID2[TAG_ID2["NAV"] = 74] = "NAV";
  TAG_ID2[TAG_ID2["NOBR"] = 75] = "NOBR";
  TAG_ID2[TAG_ID2["NOFRAMES"] = 76] = "NOFRAMES";
  TAG_ID2[TAG_ID2["NOEMBED"] = 77] = "NOEMBED";
  TAG_ID2[TAG_ID2["NOSCRIPT"] = 78] = "NOSCRIPT";
  TAG_ID2[TAG_ID2["OBJECT"] = 79] = "OBJECT";
  TAG_ID2[TAG_ID2["OL"] = 80] = "OL";
  TAG_ID2[TAG_ID2["OPTGROUP"] = 81] = "OPTGROUP";
  TAG_ID2[TAG_ID2["OPTION"] = 82] = "OPTION";
  TAG_ID2[TAG_ID2["P"] = 83] = "P";
  TAG_ID2[TAG_ID2["PARAM"] = 84] = "PARAM";
  TAG_ID2[TAG_ID2["PLAINTEXT"] = 85] = "PLAINTEXT";
  TAG_ID2[TAG_ID2["PRE"] = 86] = "PRE";
  TAG_ID2[TAG_ID2["RB"] = 87] = "RB";
  TAG_ID2[TAG_ID2["RP"] = 88] = "RP";
  TAG_ID2[TAG_ID2["RT"] = 89] = "RT";
  TAG_ID2[TAG_ID2["RTC"] = 90] = "RTC";
  TAG_ID2[TAG_ID2["RUBY"] = 91] = "RUBY";
  TAG_ID2[TAG_ID2["S"] = 92] = "S";
  TAG_ID2[TAG_ID2["SCRIPT"] = 93] = "SCRIPT";
  TAG_ID2[TAG_ID2["SEARCH"] = 94] = "SEARCH";
  TAG_ID2[TAG_ID2["SECTION"] = 95] = "SECTION";
  TAG_ID2[TAG_ID2["SELECT"] = 96] = "SELECT";
  TAG_ID2[TAG_ID2["SOURCE"] = 97] = "SOURCE";
  TAG_ID2[TAG_ID2["SMALL"] = 98] = "SMALL";
  TAG_ID2[TAG_ID2["SPAN"] = 99] = "SPAN";
  TAG_ID2[TAG_ID2["STRIKE"] = 100] = "STRIKE";
  TAG_ID2[TAG_ID2["STRONG"] = 101] = "STRONG";
  TAG_ID2[TAG_ID2["STYLE"] = 102] = "STYLE";
  TAG_ID2[TAG_ID2["SUB"] = 103] = "SUB";
  TAG_ID2[TAG_ID2["SUMMARY"] = 104] = "SUMMARY";
  TAG_ID2[TAG_ID2["SUP"] = 105] = "SUP";
  TAG_ID2[TAG_ID2["TABLE"] = 106] = "TABLE";
  TAG_ID2[TAG_ID2["TBODY"] = 107] = "TBODY";
  TAG_ID2[TAG_ID2["TEMPLATE"] = 108] = "TEMPLATE";
  TAG_ID2[TAG_ID2["TEXTAREA"] = 109] = "TEXTAREA";
  TAG_ID2[TAG_ID2["TFOOT"] = 110] = "TFOOT";
  TAG_ID2[TAG_ID2["TD"] = 111] = "TD";
  TAG_ID2[TAG_ID2["TH"] = 112] = "TH";
  TAG_ID2[TAG_ID2["THEAD"] = 113] = "THEAD";
  TAG_ID2[TAG_ID2["TITLE"] = 114] = "TITLE";
  TAG_ID2[TAG_ID2["TR"] = 115] = "TR";
  TAG_ID2[TAG_ID2["TRACK"] = 116] = "TRACK";
  TAG_ID2[TAG_ID2["TT"] = 117] = "TT";
  TAG_ID2[TAG_ID2["U"] = 118] = "U";
  TAG_ID2[TAG_ID2["UL"] = 119] = "UL";
  TAG_ID2[TAG_ID2["SVG"] = 120] = "SVG";
  TAG_ID2[TAG_ID2["VAR"] = 121] = "VAR";
  TAG_ID2[TAG_ID2["WBR"] = 122] = "WBR";
  TAG_ID2[TAG_ID2["XMP"] = 123] = "XMP";
})(TAG_ID || (TAG_ID = {}));
var TAG_NAME_TO_ID = /* @__PURE__ */ new Map([
  [TAG_NAMES.A, TAG_ID.A],
  [TAG_NAMES.ADDRESS, TAG_ID.ADDRESS],
  [TAG_NAMES.ANNOTATION_XML, TAG_ID.ANNOTATION_XML],
  [TAG_NAMES.APPLET, TAG_ID.APPLET],
  [TAG_NAMES.AREA, TAG_ID.AREA],
  [TAG_NAMES.ARTICLE, TAG_ID.ARTICLE],
  [TAG_NAMES.ASIDE, TAG_ID.ASIDE],
  [TAG_NAMES.B, TAG_ID.B],
  [TAG_NAMES.BASE, TAG_ID.BASE],
  [TAG_NAMES.BASEFONT, TAG_ID.BASEFONT],
  [TAG_NAMES.BGSOUND, TAG_ID.BGSOUND],
  [TAG_NAMES.BIG, TAG_ID.BIG],
  [TAG_NAMES.BLOCKQUOTE, TAG_ID.BLOCKQUOTE],
  [TAG_NAMES.BODY, TAG_ID.BODY],
  [TAG_NAMES.BR, TAG_ID.BR],
  [TAG_NAMES.BUTTON, TAG_ID.BUTTON],
  [TAG_NAMES.CAPTION, TAG_ID.CAPTION],
  [TAG_NAMES.CENTER, TAG_ID.CENTER],
  [TAG_NAMES.CODE, TAG_ID.CODE],
  [TAG_NAMES.COL, TAG_ID.COL],
  [TAG_NAMES.COLGROUP, TAG_ID.COLGROUP],
  [TAG_NAMES.DD, TAG_ID.DD],
  [TAG_NAMES.DESC, TAG_ID.DESC],
  [TAG_NAMES.DETAILS, TAG_ID.DETAILS],
  [TAG_NAMES.DIALOG, TAG_ID.DIALOG],
  [TAG_NAMES.DIR, TAG_ID.DIR],
  [TAG_NAMES.DIV, TAG_ID.DIV],
  [TAG_NAMES.DL, TAG_ID.DL],
  [TAG_NAMES.DT, TAG_ID.DT],
  [TAG_NAMES.EM, TAG_ID.EM],
  [TAG_NAMES.EMBED, TAG_ID.EMBED],
  [TAG_NAMES.FIELDSET, TAG_ID.FIELDSET],
  [TAG_NAMES.FIGCAPTION, TAG_ID.FIGCAPTION],
  [TAG_NAMES.FIGURE, TAG_ID.FIGURE],
  [TAG_NAMES.FONT, TAG_ID.FONT],
  [TAG_NAMES.FOOTER, TAG_ID.FOOTER],
  [TAG_NAMES.FOREIGN_OBJECT, TAG_ID.FOREIGN_OBJECT],
  [TAG_NAMES.FORM, TAG_ID.FORM],
  [TAG_NAMES.FRAME, TAG_ID.FRAME],
  [TAG_NAMES.FRAMESET, TAG_ID.FRAMESET],
  [TAG_NAMES.H1, TAG_ID.H1],
  [TAG_NAMES.H2, TAG_ID.H2],
  [TAG_NAMES.H3, TAG_ID.H3],
  [TAG_NAMES.H4, TAG_ID.H4],
  [TAG_NAMES.H5, TAG_ID.H5],
  [TAG_NAMES.H6, TAG_ID.H6],
  [TAG_NAMES.HEAD, TAG_ID.HEAD],
  [TAG_NAMES.HEADER, TAG_ID.HEADER],
  [TAG_NAMES.HGROUP, TAG_ID.HGROUP],
  [TAG_NAMES.HR, TAG_ID.HR],
  [TAG_NAMES.HTML, TAG_ID.HTML],
  [TAG_NAMES.I, TAG_ID.I],
  [TAG_NAMES.IMG, TAG_ID.IMG],
  [TAG_NAMES.IMAGE, TAG_ID.IMAGE],
  [TAG_NAMES.INPUT, TAG_ID.INPUT],
  [TAG_NAMES.IFRAME, TAG_ID.IFRAME],
  [TAG_NAMES.KEYGEN, TAG_ID.KEYGEN],
  [TAG_NAMES.LABEL, TAG_ID.LABEL],
  [TAG_NAMES.LI, TAG_ID.LI],
  [TAG_NAMES.LINK, TAG_ID.LINK],
  [TAG_NAMES.LISTING, TAG_ID.LISTING],
  [TAG_NAMES.MAIN, TAG_ID.MAIN],
  [TAG_NAMES.MALIGNMARK, TAG_ID.MALIGNMARK],
  [TAG_NAMES.MARQUEE, TAG_ID.MARQUEE],
  [TAG_NAMES.MATH, TAG_ID.MATH],
  [TAG_NAMES.MENU, TAG_ID.MENU],
  [TAG_NAMES.META, TAG_ID.META],
  [TAG_NAMES.MGLYPH, TAG_ID.MGLYPH],
  [TAG_NAMES.MI, TAG_ID.MI],
  [TAG_NAMES.MO, TAG_ID.MO],
  [TAG_NAMES.MN, TAG_ID.MN],
  [TAG_NAMES.MS, TAG_ID.MS],
  [TAG_NAMES.MTEXT, TAG_ID.MTEXT],
  [TAG_NAMES.NAV, TAG_ID.NAV],
  [TAG_NAMES.NOBR, TAG_ID.NOBR],
  [TAG_NAMES.NOFRAMES, TAG_ID.NOFRAMES],
  [TAG_NAMES.NOEMBED, TAG_ID.NOEMBED],
  [TAG_NAMES.NOSCRIPT, TAG_ID.NOSCRIPT],
  [TAG_NAMES.OBJECT, TAG_ID.OBJECT],
  [TAG_NAMES.OL, TAG_ID.OL],
  [TAG_NAMES.OPTGROUP, TAG_ID.OPTGROUP],
  [TAG_NAMES.OPTION, TAG_ID.OPTION],
  [TAG_NAMES.P, TAG_ID.P],
  [TAG_NAMES.PARAM, TAG_ID.PARAM],
  [TAG_NAMES.PLAINTEXT, TAG_ID.PLAINTEXT],
  [TAG_NAMES.PRE, TAG_ID.PRE],
  [TAG_NAMES.RB, TAG_ID.RB],
  [TAG_NAMES.RP, TAG_ID.RP],
  [TAG_NAMES.RT, TAG_ID.RT],
  [TAG_NAMES.RTC, TAG_ID.RTC],
  [TAG_NAMES.RUBY, TAG_ID.RUBY],
  [TAG_NAMES.S, TAG_ID.S],
  [TAG_NAMES.SCRIPT, TAG_ID.SCRIPT],
  [TAG_NAMES.SEARCH, TAG_ID.SEARCH],
  [TAG_NAMES.SECTION, TAG_ID.SECTION],
  [TAG_NAMES.SELECT, TAG_ID.SELECT],
  [TAG_NAMES.SOURCE, TAG_ID.SOURCE],
  [TAG_NAMES.SMALL, TAG_ID.SMALL],
  [TAG_NAMES.SPAN, TAG_ID.SPAN],
  [TAG_NAMES.STRIKE, TAG_ID.STRIKE],
  [TAG_NAMES.STRONG, TAG_ID.STRONG],
  [TAG_NAMES.STYLE, TAG_ID.STYLE],
  [TAG_NAMES.SUB, TAG_ID.SUB],
  [TAG_NAMES.SUMMARY, TAG_ID.SUMMARY],
  [TAG_NAMES.SUP, TAG_ID.SUP],
  [TAG_NAMES.TABLE, TAG_ID.TABLE],
  [TAG_NAMES.TBODY, TAG_ID.TBODY],
  [TAG_NAMES.TEMPLATE, TAG_ID.TEMPLATE],
  [TAG_NAMES.TEXTAREA, TAG_ID.TEXTAREA],
  [TAG_NAMES.TFOOT, TAG_ID.TFOOT],
  [TAG_NAMES.TD, TAG_ID.TD],
  [TAG_NAMES.TH, TAG_ID.TH],
  [TAG_NAMES.THEAD, TAG_ID.THEAD],
  [TAG_NAMES.TITLE, TAG_ID.TITLE],
  [TAG_NAMES.TR, TAG_ID.TR],
  [TAG_NAMES.TRACK, TAG_ID.TRACK],
  [TAG_NAMES.TT, TAG_ID.TT],
  [TAG_NAMES.U, TAG_ID.U],
  [TAG_NAMES.UL, TAG_ID.UL],
  [TAG_NAMES.SVG, TAG_ID.SVG],
  [TAG_NAMES.VAR, TAG_ID.VAR],
  [TAG_NAMES.WBR, TAG_ID.WBR],
  [TAG_NAMES.XMP, TAG_ID.XMP]
]);
var $ = TAG_ID;
var SPECIAL_ELEMENTS = {
  [NS.HTML]: /* @__PURE__ */ new Set([
    $.ADDRESS,
    $.APPLET,
    $.AREA,
    $.ARTICLE,
    $.ASIDE,
    $.BASE,
    $.BASEFONT,
    $.BGSOUND,
    $.BLOCKQUOTE,
    $.BODY,
    $.BR,
    $.BUTTON,
    $.CAPTION,
    $.CENTER,
    $.COL,
    $.COLGROUP,
    $.DD,
    $.DETAILS,
    $.DIR,
    $.DIV,
    $.DL,
    $.DT,
    $.EMBED,
    $.FIELDSET,
    $.FIGCAPTION,
    $.FIGURE,
    $.FOOTER,
    $.FORM,
    $.FRAME,
    $.FRAMESET,
    $.H1,
    $.H2,
    $.H3,
    $.H4,
    $.H5,
    $.H6,
    $.HEAD,
    $.HEADER,
    $.HGROUP,
    $.HR,
    $.HTML,
    $.IFRAME,
    $.IMG,
    $.INPUT,
    $.LI,
    $.LINK,
    $.LISTING,
    $.MAIN,
    $.MARQUEE,
    $.MENU,
    $.META,
    $.NAV,
    $.NOEMBED,
    $.NOFRAMES,
    $.NOSCRIPT,
    $.OBJECT,
    $.OL,
    $.P,
    $.PARAM,
    $.PLAINTEXT,
    $.PRE,
    $.SCRIPT,
    $.SECTION,
    $.SELECT,
    $.SOURCE,
    $.STYLE,
    $.SUMMARY,
    $.TABLE,
    $.TBODY,
    $.TD,
    $.TEMPLATE,
    $.TEXTAREA,
    $.TFOOT,
    $.TH,
    $.THEAD,
    $.TITLE,
    $.TR,
    $.TRACK,
    $.UL,
    $.WBR,
    $.XMP
  ]),
  [NS.MATHML]: /* @__PURE__ */ new Set([$.MI, $.MO, $.MN, $.MS, $.MTEXT, $.ANNOTATION_XML]),
  [NS.SVG]: /* @__PURE__ */ new Set([$.TITLE, $.FOREIGN_OBJECT, $.DESC]),
  [NS.XLINK]: /* @__PURE__ */ new Set(),
  [NS.XML]: /* @__PURE__ */ new Set(),
  [NS.XMLNS]: /* @__PURE__ */ new Set()
};
var NUMBERED_HEADERS = /* @__PURE__ */ new Set([$.H1, $.H2, $.H3, $.H4, $.H5, $.H6]);
var UNESCAPED_TEXT = /* @__PURE__ */ new Set([
  TAG_NAMES.STYLE,
  TAG_NAMES.SCRIPT,
  TAG_NAMES.XMP,
  TAG_NAMES.IFRAME,
  TAG_NAMES.NOEMBED,
  TAG_NAMES.NOFRAMES,
  TAG_NAMES.PLAINTEXT
]);

// node_modules/.pnpm/parse5@7.3.0/node_modules/parse5/dist/tokenizer/index.js
var State;
(function(State2) {
  State2[State2["DATA"] = 0] = "DATA";
  State2[State2["RCDATA"] = 1] = "RCDATA";
  State2[State2["RAWTEXT"] = 2] = "RAWTEXT";
  State2[State2["SCRIPT_DATA"] = 3] = "SCRIPT_DATA";
  State2[State2["PLAINTEXT"] = 4] = "PLAINTEXT";
  State2[State2["TAG_OPEN"] = 5] = "TAG_OPEN";
  State2[State2["END_TAG_OPEN"] = 6] = "END_TAG_OPEN";
  State2[State2["TAG_NAME"] = 7] = "TAG_NAME";
  State2[State2["RCDATA_LESS_THAN_SIGN"] = 8] = "RCDATA_LESS_THAN_SIGN";
  State2[State2["RCDATA_END_TAG_OPEN"] = 9] = "RCDATA_END_TAG_OPEN";
  State2[State2["RCDATA_END_TAG_NAME"] = 10] = "RCDATA_END_TAG_NAME";
  State2[State2["RAWTEXT_LESS_THAN_SIGN"] = 11] = "RAWTEXT_LESS_THAN_SIGN";
  State2[State2["RAWTEXT_END_TAG_OPEN"] = 12] = "RAWTEXT_END_TAG_OPEN";
  State2[State2["RAWTEXT_END_TAG_NAME"] = 13] = "RAWTEXT_END_TAG_NAME";
  State2[State2["SCRIPT_DATA_LESS_THAN_SIGN"] = 14] = "SCRIPT_DATA_LESS_THAN_SIGN";
  State2[State2["SCRIPT_DATA_END_TAG_OPEN"] = 15] = "SCRIPT_DATA_END_TAG_OPEN";
  State2[State2["SCRIPT_DATA_END_TAG_NAME"] = 16] = "SCRIPT_DATA_END_TAG_NAME";
  State2[State2["SCRIPT_DATA_ESCAPE_START"] = 17] = "SCRIPT_DATA_ESCAPE_START";
  State2[State2["SCRIPT_DATA_ESCAPE_START_DASH"] = 18] = "SCRIPT_DATA_ESCAPE_START_DASH";
  State2[State2["SCRIPT_DATA_ESCAPED"] = 19] = "SCRIPT_DATA_ESCAPED";
  State2[State2["SCRIPT_DATA_ESCAPED_DASH"] = 20] = "SCRIPT_DATA_ESCAPED_DASH";
  State2[State2["SCRIPT_DATA_ESCAPED_DASH_DASH"] = 21] = "SCRIPT_DATA_ESCAPED_DASH_DASH";
  State2[State2["SCRIPT_DATA_ESCAPED_LESS_THAN_SIGN"] = 22] = "SCRIPT_DATA_ESCAPED_LESS_THAN_SIGN";
  State2[State2["SCRIPT_DATA_ESCAPED_END_TAG_OPEN"] = 23] = "SCRIPT_DATA_ESCAPED_END_TAG_OPEN";
  State2[State2["SCRIPT_DATA_ESCAPED_END_TAG_NAME"] = 24] = "SCRIPT_DATA_ESCAPED_END_TAG_NAME";
  State2[State2["SCRIPT_DATA_DOUBLE_ESCAPE_START"] = 25] = "SCRIPT_DATA_DOUBLE_ESCAPE_START";
  State2[State2["SCRIPT_DATA_DOUBLE_ESCAPED"] = 26] = "SCRIPT_DATA_DOUBLE_ESCAPED";
  State2[State2["SCRIPT_DATA_DOUBLE_ESCAPED_DASH"] = 27] = "SCRIPT_DATA_DOUBLE_ESCAPED_DASH";
  State2[State2["SCRIPT_DATA_DOUBLE_ESCAPED_DASH_DASH"] = 28] = "SCRIPT_DATA_DOUBLE_ESCAPED_DASH_DASH";
  State2[State2["SCRIPT_DATA_DOUBLE_ESCAPED_LESS_THAN_SIGN"] = 29] = "SCRIPT_DATA_DOUBLE_ESCAPED_LESS_THAN_SIGN";
  State2[State2["SCRIPT_DATA_DOUBLE_ESCAPE_END"] = 30] = "SCRIPT_DATA_DOUBLE_ESCAPE_END";
  State2[State2["BEFORE_ATTRIBUTE_NAME"] = 31] = "BEFORE_ATTRIBUTE_NAME";
  State2[State2["ATTRIBUTE_NAME"] = 32] = "ATTRIBUTE_NAME";
  State2[State2["AFTER_ATTRIBUTE_NAME"] = 33] = "AFTER_ATTRIBUTE_NAME";
  State2[State2["BEFORE_ATTRIBUTE_VALUE"] = 34] = "BEFORE_ATTRIBUTE_VALUE";
  State2[State2["ATTRIBUTE_VALUE_DOUBLE_QUOTED"] = 35] = "ATTRIBUTE_VALUE_DOUBLE_QUOTED";
  State2[State2["ATTRIBUTE_VALUE_SINGLE_QUOTED"] = 36] = "ATTRIBUTE_VALUE_SINGLE_QUOTED";
  State2[State2["ATTRIBUTE_VALUE_UNQUOTED"] = 37] = "ATTRIBUTE_VALUE_UNQUOTED";
  State2[State2["AFTER_ATTRIBUTE_VALUE_QUOTED"] = 38] = "AFTER_ATTRIBUTE_VALUE_QUOTED";
  State2[State2["SELF_CLOSING_START_TAG"] = 39] = "SELF_CLOSING_START_TAG";
  State2[State2["BOGUS_COMMENT"] = 40] = "BOGUS_COMMENT";
  State2[State2["MARKUP_DECLARATION_OPEN"] = 41] = "MARKUP_DECLARATION_OPEN";
  State2[State2["COMMENT_START"] = 42] = "COMMENT_START";
  State2[State2["COMMENT_START_DASH"] = 43] = "COMMENT_START_DASH";
  State2[State2["COMMENT"] = 44] = "COMMENT";
  State2[State2["COMMENT_LESS_THAN_SIGN"] = 45] = "COMMENT_LESS_THAN_SIGN";
  State2[State2["COMMENT_LESS_THAN_SIGN_BANG"] = 46] = "COMMENT_LESS_THAN_SIGN_BANG";
  State2[State2["COMMENT_LESS_THAN_SIGN_BANG_DASH"] = 47] = "COMMENT_LESS_THAN_SIGN_BANG_DASH";
  State2[State2["COMMENT_LESS_THAN_SIGN_BANG_DASH_DASH"] = 48] = "COMMENT_LESS_THAN_SIGN_BANG_DASH_DASH";
  State2[State2["COMMENT_END_DASH"] = 49] = "COMMENT_END_DASH";
  State2[State2["COMMENT_END"] = 50] = "COMMENT_END";
  State2[State2["COMMENT_END_BANG"] = 51] = "COMMENT_END_BANG";
  State2[State2["DOCTYPE"] = 52] = "DOCTYPE";
  State2[State2["BEFORE_DOCTYPE_NAME"] = 53] = "BEFORE_DOCTYPE_NAME";
  State2[State2["DOCTYPE_NAME"] = 54] = "DOCTYPE_NAME";
  State2[State2["AFTER_DOCTYPE_NAME"] = 55] = "AFTER_DOCTYPE_NAME";
  State2[State2["AFTER_DOCTYPE_PUBLIC_KEYWORD"] = 56] = "AFTER_DOCTYPE_PUBLIC_KEYWORD";
  State2[State2["BEFORE_DOCTYPE_PUBLIC_IDENTIFIER"] = 57] = "BEFORE_DOCTYPE_PUBLIC_IDENTIFIER";
  State2[State2["DOCTYPE_PUBLIC_IDENTIFIER_DOUBLE_QUOTED"] = 58] = "DOCTYPE_PUBLIC_IDENTIFIER_DOUBLE_QUOTED";
  State2[State2["DOCTYPE_PUBLIC_IDENTIFIER_SINGLE_QUOTED"] = 59] = "DOCTYPE_PUBLIC_IDENTIFIER_SINGLE_QUOTED";
  State2[State2["AFTER_DOCTYPE_PUBLIC_IDENTIFIER"] = 60] = "AFTER_DOCTYPE_PUBLIC_IDENTIFIER";
  State2[State2["BETWEEN_DOCTYPE_PUBLIC_AND_SYSTEM_IDENTIFIERS"] = 61] = "BETWEEN_DOCTYPE_PUBLIC_AND_SYSTEM_IDENTIFIERS";
  State2[State2["AFTER_DOCTYPE_SYSTEM_KEYWORD"] = 62] = "AFTER_DOCTYPE_SYSTEM_KEYWORD";
  State2[State2["BEFORE_DOCTYPE_SYSTEM_IDENTIFIER"] = 63] = "BEFORE_DOCTYPE_SYSTEM_IDENTIFIER";
  State2[State2["DOCTYPE_SYSTEM_IDENTIFIER_DOUBLE_QUOTED"] = 64] = "DOCTYPE_SYSTEM_IDENTIFIER_DOUBLE_QUOTED";
  State2[State2["DOCTYPE_SYSTEM_IDENTIFIER_SINGLE_QUOTED"] = 65] = "DOCTYPE_SYSTEM_IDENTIFIER_SINGLE_QUOTED";
  State2[State2["AFTER_DOCTYPE_SYSTEM_IDENTIFIER"] = 66] = "AFTER_DOCTYPE_SYSTEM_IDENTIFIER";
  State2[State2["BOGUS_DOCTYPE"] = 67] = "BOGUS_DOCTYPE";
  State2[State2["CDATA_SECTION"] = 68] = "CDATA_SECTION";
  State2[State2["CDATA_SECTION_BRACKET"] = 69] = "CDATA_SECTION_BRACKET";
  State2[State2["CDATA_SECTION_END"] = 70] = "CDATA_SECTION_END";
  State2[State2["CHARACTER_REFERENCE"] = 71] = "CHARACTER_REFERENCE";
  State2[State2["AMBIGUOUS_AMPERSAND"] = 72] = "AMBIGUOUS_AMPERSAND";
})(State || (State = {}));
var TokenizerMode = {
  DATA: State.DATA,
  RCDATA: State.RCDATA,
  RAWTEXT: State.RAWTEXT,
  SCRIPT_DATA: State.SCRIPT_DATA,
  PLAINTEXT: State.PLAINTEXT,
  CDATA_SECTION: State.CDATA_SECTION
};

// node_modules/.pnpm/parse5@7.3.0/node_modules/parse5/dist/parser/open-element-stack.js
var IMPLICIT_END_TAG_REQUIRED = /* @__PURE__ */ new Set([TAG_ID.DD, TAG_ID.DT, TAG_ID.LI, TAG_ID.OPTGROUP, TAG_ID.OPTION, TAG_ID.P, TAG_ID.RB, TAG_ID.RP, TAG_ID.RT, TAG_ID.RTC]);
var IMPLICIT_END_TAG_REQUIRED_THOROUGHLY = /* @__PURE__ */ new Set([
  ...IMPLICIT_END_TAG_REQUIRED,
  TAG_ID.CAPTION,
  TAG_ID.COLGROUP,
  TAG_ID.TBODY,
  TAG_ID.TD,
  TAG_ID.TFOOT,
  TAG_ID.TH,
  TAG_ID.THEAD,
  TAG_ID.TR
]);
var SCOPING_ELEMENTS_HTML = /* @__PURE__ */ new Set([
  TAG_ID.APPLET,
  TAG_ID.CAPTION,
  TAG_ID.HTML,
  TAG_ID.MARQUEE,
  TAG_ID.OBJECT,
  TAG_ID.TABLE,
  TAG_ID.TD,
  TAG_ID.TEMPLATE,
  TAG_ID.TH
]);
var SCOPING_ELEMENTS_HTML_LIST = /* @__PURE__ */ new Set([...SCOPING_ELEMENTS_HTML, TAG_ID.OL, TAG_ID.UL]);
var SCOPING_ELEMENTS_HTML_BUTTON = /* @__PURE__ */ new Set([...SCOPING_ELEMENTS_HTML, TAG_ID.BUTTON]);
var SCOPING_ELEMENTS_MATHML = /* @__PURE__ */ new Set([TAG_ID.ANNOTATION_XML, TAG_ID.MI, TAG_ID.MN, TAG_ID.MO, TAG_ID.MS, TAG_ID.MTEXT]);
var SCOPING_ELEMENTS_SVG = /* @__PURE__ */ new Set([TAG_ID.DESC, TAG_ID.FOREIGN_OBJECT, TAG_ID.TITLE]);
var TABLE_ROW_CONTEXT = /* @__PURE__ */ new Set([TAG_ID.TR, TAG_ID.TEMPLATE, TAG_ID.HTML]);
var TABLE_BODY_CONTEXT = /* @__PURE__ */ new Set([TAG_ID.TBODY, TAG_ID.TFOOT, TAG_ID.THEAD, TAG_ID.TEMPLATE, TAG_ID.HTML]);
var TABLE_CONTEXT = /* @__PURE__ */ new Set([TAG_ID.TABLE, TAG_ID.TEMPLATE, TAG_ID.HTML]);
var TABLE_CELLS = /* @__PURE__ */ new Set([TAG_ID.TD, TAG_ID.TH]);

// node_modules/.pnpm/parse5@7.3.0/node_modules/parse5/dist/parser/formatting-element-list.js
var EntryType;
(function(EntryType2) {
  EntryType2[EntryType2["Marker"] = 0] = "Marker";
  EntryType2[EntryType2["Element"] = 1] = "Element";
})(EntryType || (EntryType = {}));
var MARKER = { type: EntryType.Marker };

// node_modules/.pnpm/parse5@7.3.0/node_modules/parse5/dist/common/doctype.js
var QUIRKS_MODE_PUBLIC_ID_PREFIXES = [
  "+//silmaril//dtd html pro v0r11 19970101//",
  "-//as//dtd html 3.0 aswedit + extensions//",
  "-//advasoft ltd//dtd html 3.0 aswedit + extensions//",
  "-//ietf//dtd html 2.0 level 1//",
  "-//ietf//dtd html 2.0 level 2//",
  "-//ietf//dtd html 2.0 strict level 1//",
  "-//ietf//dtd html 2.0 strict level 2//",
  "-//ietf//dtd html 2.0 strict//",
  "-//ietf//dtd html 2.0//",
  "-//ietf//dtd html 2.1e//",
  "-//ietf//dtd html 3.0//",
  "-//ietf//dtd html 3.2 final//",
  "-//ietf//dtd html 3.2//",
  "-//ietf//dtd html 3//",
  "-//ietf//dtd html level 0//",
  "-//ietf//dtd html level 1//",
  "-//ietf//dtd html level 2//",
  "-//ietf//dtd html level 3//",
  "-//ietf//dtd html strict level 0//",
  "-//ietf//dtd html strict level 1//",
  "-//ietf//dtd html strict level 2//",
  "-//ietf//dtd html strict level 3//",
  "-//ietf//dtd html strict//",
  "-//ietf//dtd html//",
  "-//metrius//dtd metrius presentational//",
  "-//microsoft//dtd internet explorer 2.0 html strict//",
  "-//microsoft//dtd internet explorer 2.0 html//",
  "-//microsoft//dtd internet explorer 2.0 tables//",
  "-//microsoft//dtd internet explorer 3.0 html strict//",
  "-//microsoft//dtd internet explorer 3.0 html//",
  "-//microsoft//dtd internet explorer 3.0 tables//",
  "-//netscape comm. corp.//dtd html//",
  "-//netscape comm. corp.//dtd strict html//",
  "-//o'reilly and associates//dtd html 2.0//",
  "-//o'reilly and associates//dtd html extended 1.0//",
  "-//o'reilly and associates//dtd html extended relaxed 1.0//",
  "-//sq//dtd html 2.0 hotmetal + extensions//",
  "-//softquad software//dtd hotmetal pro 6.0::19990601::extensions to html 4.0//",
  "-//softquad//dtd hotmetal pro 4.0::19971010::extensions to html 4.0//",
  "-//spyglass//dtd html 2.0 extended//",
  "-//sun microsystems corp.//dtd hotjava html//",
  "-//sun microsystems corp.//dtd hotjava strict html//",
  "-//w3c//dtd html 3 1995-03-24//",
  "-//w3c//dtd html 3.2 draft//",
  "-//w3c//dtd html 3.2 final//",
  "-//w3c//dtd html 3.2//",
  "-//w3c//dtd html 3.2s draft//",
  "-//w3c//dtd html 4.0 frameset//",
  "-//w3c//dtd html 4.0 transitional//",
  "-//w3c//dtd html experimental 19960712//",
  "-//w3c//dtd html experimental 970421//",
  "-//w3c//dtd w3 html//",
  "-//w3o//dtd w3 html 3.0//",
  "-//webtechs//dtd mozilla html 2.0//",
  "-//webtechs//dtd mozilla html//"
];
var QUIRKS_MODE_NO_SYSTEM_ID_PUBLIC_ID_PREFIXES = [
  ...QUIRKS_MODE_PUBLIC_ID_PREFIXES,
  "-//w3c//dtd html 4.01 frameset//",
  "-//w3c//dtd html 4.01 transitional//"
];
var LIMITED_QUIRKS_PUBLIC_ID_PREFIXES = ["-//w3c//dtd xhtml 1.0 frameset//", "-//w3c//dtd xhtml 1.0 transitional//"];
var LIMITED_QUIRKS_WITH_SYSTEM_ID_PUBLIC_ID_PREFIXES = [
  ...LIMITED_QUIRKS_PUBLIC_ID_PREFIXES,
  "-//w3c//dtd html 4.01 frameset//",
  "-//w3c//dtd html 4.01 transitional//"
];

// node_modules/.pnpm/parse5@7.3.0/node_modules/parse5/dist/common/foreign-content.js
var SVG_ATTRS_ADJUSTMENT_MAP = new Map([
  "attributeName",
  "attributeType",
  "baseFrequency",
  "baseProfile",
  "calcMode",
  "clipPathUnits",
  "diffuseConstant",
  "edgeMode",
  "filterUnits",
  "glyphRef",
  "gradientTransform",
  "gradientUnits",
  "kernelMatrix",
  "kernelUnitLength",
  "keyPoints",
  "keySplines",
  "keyTimes",
  "lengthAdjust",
  "limitingConeAngle",
  "markerHeight",
  "markerUnits",
  "markerWidth",
  "maskContentUnits",
  "maskUnits",
  "numOctaves",
  "pathLength",
  "patternContentUnits",
  "patternTransform",
  "patternUnits",
  "pointsAtX",
  "pointsAtY",
  "pointsAtZ",
  "preserveAlpha",
  "preserveAspectRatio",
  "primitiveUnits",
  "refX",
  "refY",
  "repeatCount",
  "repeatDur",
  "requiredExtensions",
  "requiredFeatures",
  "specularConstant",
  "specularExponent",
  "spreadMethod",
  "startOffset",
  "stdDeviation",
  "stitchTiles",
  "surfaceScale",
  "systemLanguage",
  "tableValues",
  "targetX",
  "targetY",
  "textLength",
  "viewBox",
  "viewTarget",
  "xChannelSelector",
  "yChannelSelector",
  "zoomAndPan"
].map((attr) => [attr.toLowerCase(), attr]));
var XML_ATTRS_ADJUSTMENT_MAP = /* @__PURE__ */ new Map([
  ["xlink:actuate", { prefix: "xlink", name: "actuate", namespace: NS.XLINK }],
  ["xlink:arcrole", { prefix: "xlink", name: "arcrole", namespace: NS.XLINK }],
  ["xlink:href", { prefix: "xlink", name: "href", namespace: NS.XLINK }],
  ["xlink:role", { prefix: "xlink", name: "role", namespace: NS.XLINK }],
  ["xlink:show", { prefix: "xlink", name: "show", namespace: NS.XLINK }],
  ["xlink:title", { prefix: "xlink", name: "title", namespace: NS.XLINK }],
  ["xlink:type", { prefix: "xlink", name: "type", namespace: NS.XLINK }],
  ["xml:lang", { prefix: "xml", name: "lang", namespace: NS.XML }],
  ["xml:space", { prefix: "xml", name: "space", namespace: NS.XML }],
  ["xmlns", { prefix: "", name: "xmlns", namespace: NS.XMLNS }],
  ["xmlns:xlink", { prefix: "xmlns", name: "xlink", namespace: NS.XMLNS }]
]);
var SVG_TAG_NAMES_ADJUSTMENT_MAP = new Map([
  "altGlyph",
  "altGlyphDef",
  "altGlyphItem",
  "animateColor",
  "animateMotion",
  "animateTransform",
  "clipPath",
  "feBlend",
  "feColorMatrix",
  "feComponentTransfer",
  "feComposite",
  "feConvolveMatrix",
  "feDiffuseLighting",
  "feDisplacementMap",
  "feDistantLight",
  "feFlood",
  "feFuncA",
  "feFuncB",
  "feFuncG",
  "feFuncR",
  "feGaussianBlur",
  "feImage",
  "feMerge",
  "feMergeNode",
  "feMorphology",
  "feOffset",
  "fePointLight",
  "feSpecularLighting",
  "feSpotLight",
  "feTile",
  "feTurbulence",
  "foreignObject",
  "glyphRef",
  "linearGradient",
  "radialGradient",
  "textPath"
].map((tn) => [tn.toLowerCase(), tn]));
var EXITS_FOREIGN_CONTENT = /* @__PURE__ */ new Set([
  TAG_ID.B,
  TAG_ID.BIG,
  TAG_ID.BLOCKQUOTE,
  TAG_ID.BODY,
  TAG_ID.BR,
  TAG_ID.CENTER,
  TAG_ID.CODE,
  TAG_ID.DD,
  TAG_ID.DIV,
  TAG_ID.DL,
  TAG_ID.DT,
  TAG_ID.EM,
  TAG_ID.EMBED,
  TAG_ID.H1,
  TAG_ID.H2,
  TAG_ID.H3,
  TAG_ID.H4,
  TAG_ID.H5,
  TAG_ID.H6,
  TAG_ID.HEAD,
  TAG_ID.HR,
  TAG_ID.I,
  TAG_ID.IMG,
  TAG_ID.LI,
  TAG_ID.LISTING,
  TAG_ID.MENU,
  TAG_ID.META,
  TAG_ID.NOBR,
  TAG_ID.OL,
  TAG_ID.P,
  TAG_ID.PRE,
  TAG_ID.RUBY,
  TAG_ID.S,
  TAG_ID.SMALL,
  TAG_ID.SPAN,
  TAG_ID.STRONG,
  TAG_ID.STRIKE,
  TAG_ID.SUB,
  TAG_ID.SUP,
  TAG_ID.TABLE,
  TAG_ID.TT,
  TAG_ID.U,
  TAG_ID.UL,
  TAG_ID.VAR
]);

// node_modules/.pnpm/parse5@7.3.0/node_modules/parse5/dist/parser/index.js
var InsertionMode;
(function(InsertionMode2) {
  InsertionMode2[InsertionMode2["INITIAL"] = 0] = "INITIAL";
  InsertionMode2[InsertionMode2["BEFORE_HTML"] = 1] = "BEFORE_HTML";
  InsertionMode2[InsertionMode2["BEFORE_HEAD"] = 2] = "BEFORE_HEAD";
  InsertionMode2[InsertionMode2["IN_HEAD"] = 3] = "IN_HEAD";
  InsertionMode2[InsertionMode2["IN_HEAD_NO_SCRIPT"] = 4] = "IN_HEAD_NO_SCRIPT";
  InsertionMode2[InsertionMode2["AFTER_HEAD"] = 5] = "AFTER_HEAD";
  InsertionMode2[InsertionMode2["IN_BODY"] = 6] = "IN_BODY";
  InsertionMode2[InsertionMode2["TEXT"] = 7] = "TEXT";
  InsertionMode2[InsertionMode2["IN_TABLE"] = 8] = "IN_TABLE";
  InsertionMode2[InsertionMode2["IN_TABLE_TEXT"] = 9] = "IN_TABLE_TEXT";
  InsertionMode2[InsertionMode2["IN_CAPTION"] = 10] = "IN_CAPTION";
  InsertionMode2[InsertionMode2["IN_COLUMN_GROUP"] = 11] = "IN_COLUMN_GROUP";
  InsertionMode2[InsertionMode2["IN_TABLE_BODY"] = 12] = "IN_TABLE_BODY";
  InsertionMode2[InsertionMode2["IN_ROW"] = 13] = "IN_ROW";
  InsertionMode2[InsertionMode2["IN_CELL"] = 14] = "IN_CELL";
  InsertionMode2[InsertionMode2["IN_SELECT"] = 15] = "IN_SELECT";
  InsertionMode2[InsertionMode2["IN_SELECT_IN_TABLE"] = 16] = "IN_SELECT_IN_TABLE";
  InsertionMode2[InsertionMode2["IN_TEMPLATE"] = 17] = "IN_TEMPLATE";
  InsertionMode2[InsertionMode2["AFTER_BODY"] = 18] = "AFTER_BODY";
  InsertionMode2[InsertionMode2["IN_FRAMESET"] = 19] = "IN_FRAMESET";
  InsertionMode2[InsertionMode2["AFTER_FRAMESET"] = 20] = "AFTER_FRAMESET";
  InsertionMode2[InsertionMode2["AFTER_AFTER_BODY"] = 21] = "AFTER_AFTER_BODY";
  InsertionMode2[InsertionMode2["AFTER_AFTER_FRAMESET"] = 22] = "AFTER_AFTER_FRAMESET";
})(InsertionMode || (InsertionMode = {}));
var TABLE_STRUCTURE_TAGS = /* @__PURE__ */ new Set([TAG_ID.TABLE, TAG_ID.TBODY, TAG_ID.TFOOT, TAG_ID.THEAD, TAG_ID.TR]);
var TABLE_VOID_ELEMENTS = /* @__PURE__ */ new Set([TAG_ID.CAPTION, TAG_ID.COL, TAG_ID.COLGROUP, TAG_ID.TBODY, TAG_ID.TD, TAG_ID.TFOOT, TAG_ID.TH, TAG_ID.THEAD, TAG_ID.TR]);

// node_modules/.pnpm/entities@6.0.1/node_modules/entities/dist/esm/escape.js
var getCodePoint = (
  // eslint-disable-next-line @typescript-eslint/no-unnecessary-condition
  String.prototype.codePointAt == null ? (c, index) => (c.charCodeAt(index) & 64512) === 55296 ? (c.charCodeAt(index) - 55296) * 1024 + c.charCodeAt(index + 1) - 56320 + 65536 : c.charCodeAt(index) : (
    // http://mathiasbynens.be/notes/javascript-encoding#surrogate-formulae
    (input, index) => input.codePointAt(index)
  )
);

// node_modules/.pnpm/parse5@7.3.0/node_modules/parse5/dist/serializer/index.js
var VOID_ELEMENTS = /* @__PURE__ */ new Set([
  TAG_NAMES.AREA,
  TAG_NAMES.BASE,
  TAG_NAMES.BASEFONT,
  TAG_NAMES.BGSOUND,
  TAG_NAMES.BR,
  TAG_NAMES.COL,
  TAG_NAMES.EMBED,
  TAG_NAMES.FRAME,
  TAG_NAMES.HR,
  TAG_NAMES.IMG,
  TAG_NAMES.INPUT,
  TAG_NAMES.KEYGEN,
  TAG_NAMES.LINK,
  TAG_NAMES.META,
  TAG_NAMES.PARAM,
  TAG_NAMES.SOURCE,
  TAG_NAMES.TRACK,
  TAG_NAMES.WBR
]);

// node_modules/.pnpm/@formepdf+react@0.25.0_react@19.2.7/node_modules/@formepdf/react/dist/template-proxy.js
var EACH_MARKER = Symbol.for("forme:each");
var EXPR_MARKER = Symbol.for("forme:expr");

// packages/invoices/src/pdf.ts
var import_react6 = __toESM(require_react(), 1);

// packages/invoices/src/pdf-logo.ts
var MAX_BYTES = 1024 * 1024;
var MAX_DIMENSION = 4096;
var MAX_PIXELS = 4e6;
var LOGO_KEYS = /* @__PURE__ */ new Set(["mimeType", "dataBase64", "width", "height"]);
var BASE64 = /^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/;
function invalid2() {
  throw new InvoiceValidationError(
    "logo: must be a valid local PNG or JPEG no larger than 1 MiB, 4096 pixels per side and 4 million pixels"
  );
}
function validateInvoicePdfLogo(value) {
  if (!value || typeof value !== "object" || Array.isArray(value)) invalid2();
  const input = value;
  if (Object.keys(input).some((key) => !LOGO_KEYS.has(key))) invalid2();
  const { mimeType, dataBase64, width, height } = input;
  if (mimeType !== "image/png" && mimeType !== "image/jpeg" || typeof dataBase64 !== "string" || dataBase64.length === 0 || dataBase64.length > Math.ceil(MAX_BYTES / 3) * 4 || !BASE64.test(dataBase64) || typeof width !== "number" || typeof height !== "number" || !Number.isSafeInteger(width) || !Number.isSafeInteger(height) || width < 1 || height < 1 || width > MAX_DIMENSION || height > MAX_DIMENSION || width * height > MAX_PIXELS)
    invalid2();
  let binary;
  try {
    binary = atob(dataBase64);
  } catch {
    invalid2();
  }
  if (binary.length < 1 || binary.length > MAX_BYTES || btoa(binary) !== dataBase64) invalid2();
  const bytes = Uint8Array.from(binary, (character) => character.charCodeAt(0));
  const dimensions = mimeType === "image/png" ? pngDimensions(bytes) : jpegDimensions(bytes);
  if (dimensions.width !== width || dimensions.height !== height) invalid2();
  return { mimeType, dataBase64, width, height };
}
function crc32(bytes) {
  let crc = 4294967295;
  for (const byte of bytes) {
    crc ^= byte;
    for (let bit = 0; bit < 8; bit++) crc = crc >>> 1 ^ (crc & 1 ? 3988292384 : 0);
  }
  return (crc ^ 4294967295) >>> 0;
}
function pngDimensions(bytes) {
  const signature = [137, 80, 78, 71, 13, 10, 26, 10];
  if (signature.some((byte, index) => bytes[index] !== byte)) invalid2();
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  let position = 8;
  let dimensions;
  let colorType = -1;
  let seenPalette = false;
  let seenData = false;
  let endedData = false;
  let dataLength = 0;
  while (position < bytes.length) {
    if (bytes.length - position < 12) invalid2();
    const length = view.getUint32(position);
    if (length > bytes.length - position - 12) invalid2();
    const typeStart = position + 4;
    const dataStart = typeStart + 4;
    const dataEnd = dataStart + length;
    const end = dataEnd + 4;
    const type = String.fromCharCode(...bytes.subarray(typeStart, dataStart));
    if (!/^[A-Za-z]{2}[A-Z][A-Za-z]$/.test(type)) invalid2();
    if (crc32(bytes.subarray(typeStart, dataEnd)) !== view.getUint32(dataEnd)) invalid2();
    if (!dimensions && type !== "IHDR") invalid2();
    if (type === "IHDR") {
      if (dimensions || length !== 13 || position !== 8) invalid2();
      const bitDepth = bytes[dataStart + 8];
      colorType = bytes[dataStart + 9] ?? -1;
      const depths = {
        0: [1, 2, 4, 8, 16],
        2: [8, 16],
        3: [1, 2, 4, 8],
        4: [8, 16],
        6: [8, 16]
      };
      if (bitDepth === void 0 || !depths[colorType]?.includes(bitDepth) || bytes[dataStart + 10] !== 0 || bytes[dataStart + 11] !== 0 || bytes[dataStart + 12] !== 0 && bytes[dataStart + 12] !== 1)
        invalid2();
      dimensions = { width: view.getUint32(dataStart), height: view.getUint32(dataStart + 4) };
    } else if (type === "PLTE") {
      if (seenPalette || seenData || colorType === 0 || colorType === 4 || length === 0 || length > 768 || length % 3 !== 0)
        invalid2();
      seenPalette = true;
    } else if (type === "IDAT") {
      if (endedData || colorType === 3 && !seenPalette) invalid2();
      seenData = true;
      dataLength += length;
    } else if (type === "IEND") {
      if (!dimensions || !seenData || dataLength === 0 || length !== 0 || end !== bytes.length)
        invalid2();
      return dimensions;
    } else {
      if (type[0] === type[0]?.toUpperCase() || ["acTL", "fcTL", "fdAT"].includes(type)) invalid2();
      if (seenData) endedData = true;
    }
    position = end;
  }
  invalid2();
}
function jpegDimensions(bytes) {
  if (bytes.length < 4 || bytes[0] !== 255 || bytes[1] !== 216) invalid2();
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  let position = 2;
  let inScan = false;
  let seenScan = false;
  let scanDataBytes = 0;
  let dimensions;
  while (position < bytes.length) {
    if (inScan && bytes[position] !== 255) {
      scanDataBytes++;
      position++;
      continue;
    }
    if (bytes[position] !== 255) invalid2();
    while (position < bytes.length && bytes[position] === 255) position++;
    if (position >= bytes.length) invalid2();
    const marker = bytes[position++];
    if (marker === void 0) invalid2();
    if (inScan && marker === 0) {
      scanDataBytes++;
      continue;
    }
    if (inScan && marker >= 208 && marker <= 215) continue;
    if (inScan && scanDataBytes === 0) invalid2();
    inScan = false;
    if (marker === 217) {
      if (!dimensions || !seenScan || position !== bytes.length) invalid2();
      return dimensions;
    }
    if (marker === 0 || marker === 216 || marker >= 208 && marker <= 215) invalid2();
    if (marker === 1) continue;
    if (marker < 192 || bytes.length - position < 2) invalid2();
    const length = view.getUint16(position);
    if (length < 2 || length > bytes.length - position) invalid2();
    if ([192, 193, 194].includes(marker)) {
      if (dimensions || length < 8 || bytes[position + 2] !== 8) invalid2();
      const components = bytes[position + 7];
      if (!components || ![1, 3, 4].includes(components) || length !== 8 + components * 3)
        invalid2();
      dimensions = { width: view.getUint16(position + 5), height: view.getUint16(position + 3) };
    } else if (marker === 218) {
      const components = bytes[position + 2];
      if (!dimensions || !components || components > 4 || length !== 6 + components * 2) invalid2();
      seenScan = true;
      inScan = true;
      scanDataBytes = 0;
    } else if (![196, 219, 221, 254].includes(marker) && !(marker >= 224 && marker <= 239)) {
      invalid2();
    }
    position += length;
  }
  invalid2();
}

// packages/invoices/src/pdfcn/font-coverage.ts
var NOTO_SANS_RANGES = [
  [32, 126],
  [160, 887],
  [890, 895],
  [900, 906],
  [908, 908],
  [910, 929],
  [931, 993],
  [1008, 1327],
  [4347, 4347],
  [6832, 6848],
  [6853, 6853],
  [6855, 6862],
  [7296, 7304],
  [7424, 7673],
  [7675, 7957],
  [7960, 7965],
  [7968, 8005],
  [8008, 8013],
  [8016, 8023],
  [8025, 8025],
  [8027, 8027],
  [8029, 8029],
  [8031, 8061],
  [8064, 8116],
  [8118, 8132],
  [8134, 8147],
  [8150, 8155],
  [8157, 8175],
  [8178, 8180],
  [8182, 8190],
  [8192, 8292],
  [8294, 8305],
  [8308, 8334],
  [8336, 8348],
  [8352, 8384],
  [8432, 8432],
  [8448, 8543],
  [8579, 8580],
  [8585, 8585],
  [8722, 8722],
  [9676, 9676],
  [11360, 11391],
  [11744, 11869],
  [42560, 42655],
  [42752, 42954],
  [42960, 42961],
  [42963, 42963],
  [42965, 42969],
  [42994, 43007],
  [43310, 43310],
  [43824, 43883],
  [64256, 64262],
  [65024, 65024],
  [65056, 65071],
  [65279, 65279],
  [65532, 65533]
];
var InvoicePdfUnsupportedTextError = class extends InvoiceValidationError {
  field;
  codePoint;
  constructor(field, codePoint) {
    super(
      `${field}: the PDF font does not support U+${codePoint.toString(16).toUpperCase().padStart(4, "0")}. Use supported Latin, Greek or Cyrillic text for export; the saved invoice has not been changed.`
    );
    this.name = "InvoicePdfUnsupportedTextError";
    this.field = field;
    this.codePoint = codePoint;
  }
};
function assertInvoicePdfTextSupported(value, field) {
  for (const character of value) {
    const codePoint = character.codePointAt(0);
    if (codePoint === void 0 || codePoint === 9 || codePoint === 10 || codePoint === 13)
      continue;
    if (!NOTO_SANS_RANGES.some(([start, end]) => codePoint >= start && codePoint <= end)) {
      throw new InvoicePdfUnsupportedTextError(field, codePoint);
    }
  }
}

// packages/invoices/src/pdf-policy.ts
function assertInvoicePdfReady(draft, sellerName, logo) {
  if (logo !== void 0) validateInvoicePdfLogo(logo);
  assertInvoicePdfTextSupported(sellerName, "sellerName");
  for (const field of [
    "number",
    "sellerAddress",
    "customerName",
    "customerAddress",
    "notes",
    "paymentInstructions"
  ]) {
    assertInvoicePdfTextSupported(draft[field], field);
  }
  for (const [index, item] of draft.lines.entries())
    assertInvoicePdfTextSupported(item.description, `lines[${index}].description`);
}

// packages/invoices/src/pdfcn/components.ts
var import_react4 = __toESM(require_react(), 1);
var theme = {
  colors: {
    foreground: "#172b2b",
    mutedForeground: "#526661",
    background: "#ffffff",
    primary: "#204e43",
    primaryForeground: "#ffffff",
    border: "#dfe8e3",
    muted: "#f3f6f3"
  },
  spacing: { sectionGap: 18, paragraphGap: 4 }
};
function Text2({ children, style = {} }) {
  return (0, import_react4.createElement)(
    Text,
    {
      style: {
        color: theme.colors.foreground,
        fontFamily: "Noto Sans",
        fontSize: 10,
        lineHeight: 1.35,
        marginBottom: theme.spacing.paragraphGap,
        ...style
      }
    },
    children
  );
}
function PageHeader({
  title,
  subtitle,
  rightText,
  logo
}) {
  const heading = [
    (0, import_react4.createElement)(
      Text2,
      {
        key: "title",
        style: {
          fontSize: 22,
          fontWeight: 700,
          color: theme.colors.primaryForeground,
          marginBottom: 8
        }
      },
      title
    ),
    (0, import_react4.createElement)(
      Text2,
      {
        key: "subtitle",
        style: { fontSize: 9, color: theme.colors.primaryForeground, marginBottom: 0 }
      },
      subtitle
    )
  ];
  const scale = logo ? Math.min(120 / logo.width, 56 / logo.height) : 1;
  return (0, import_react4.createElement)(
    View,
    {
      wrap: false,
      style: {
        backgroundColor: theme.colors.primary,
        padding: 16,
        marginBottom: theme.spacing.sectionGap,
        borderRadius: 4
      }
    },
    ...logo ? [
      (0, import_react4.createElement)(
        View,
        { key: "brand", style: { flexDirection: "row", gap: 16, alignItems: "flex-start" } },
        (0, import_react4.createElement)(View, { style: { flex: 1 } }, ...heading),
        (0, import_react4.createElement)(
          View,
          {
            style: {
              width: 128,
              height: 64,
              padding: 4,
              backgroundColor: "#ffffff",
              borderRadius: 2,
              alignItems: "center",
              justifyContent: "center"
            }
          },
          (0, import_react4.createElement)(Image, {
            src: `data:${logo.mimeType};base64,${logo.dataBase64}`,
            width: logo.width * scale,
            height: logo.height * scale,
            alt: "Company logo"
          })
        )
      )
    ] : heading,
    (0, import_react4.createElement)(
      Text2,
      {
        style: {
          fontSize: 9,
          color: theme.colors.primaryForeground,
          marginTop: 12,
          marginBottom: 0
        }
      },
      rightText
    )
  );
}
function PageFooter({ leftText }) {
  return (0, import_react4.createElement)(
    Fixed,
    {
      position: "footer",
      style: { borderTopWidth: 0.5, borderTopColor: theme.colors.border, paddingTop: 9 }
    },
    (0, import_react4.createElement)(
      View,
      { style: { flexDirection: "row", justifyContent: "space-between" } },
      (0, import_react4.createElement)(
        Text2,
        { style: { fontSize: 8, color: theme.colors.mutedForeground, marginBottom: 0 } },
        leftText
      ),
      (0, import_react4.createElement)(
        Text2,
        { style: { fontSize: 8, color: theme.colors.mutedForeground, marginBottom: 0 } },
        "Page {{pageNumber}} of {{totalPages}}"
      )
    )
  );
}
function TableCell({
  children,
  align = "left",
  header = false
}) {
  return (0, import_react4.createElement)(
    Cell,
    { style: { paddingTop: 9, paddingBottom: 9, paddingLeft: 8, paddingRight: 8 } },
    (0, import_react4.createElement)(
      Text2,
      {
        style: {
          fontSize: 9,
          marginBottom: 0,
          textAlign: align,
          color: header ? theme.colors.primaryForeground : theme.colors.foreground,
          fontWeight: header ? 700 : 400
        }
      },
      children
    )
  );
}
function TableRow({
  children,
  header = false,
  stripe = false
}) {
  return (0, import_react4.createElement)(
    Row,
    {
      header,
      style: {
        backgroundColor: header ? theme.colors.primary : stripe ? theme.colors.muted : theme.colors.background,
        borderBottomWidth: 0.5,
        borderBottomColor: theme.colors.border
      }
    },
    children
  );
}
function Table2({ children }) {
  return (0, import_react4.createElement)(
    Table,
    {
      columns: [
        { width: { fraction: 0.38 } },
        { width: { fraction: 0.08 } },
        { width: { fraction: 0.19 } },
        { width: { fraction: 0.17 } },
        { width: { fraction: 0.18 } }
      ]
    },
    children
  );
}
function KeyValue({
  items
}) {
  return (0, import_react4.createElement)(
    View,
    { wrap: false, style: { width: 245 } },
    ...items.map(
      (item) => (0, import_react4.createElement)(
        View,
        {
          key: item.key,
          style: {
            flexDirection: "row",
            justifyContent: "space-between",
            borderBottomWidth: 0.5,
            borderBottomColor: theme.colors.border,
            paddingTop: 8,
            paddingBottom: 8
          }
        },
        (0, import_react4.createElement)(
          Text2,
          {
            style: {
              fontSize: item.prominent ? 13 : 10,
              fontWeight: item.prominent ? 700 : 400,
              marginBottom: 0
            }
          },
          item.key
        ),
        (0, import_react4.createElement)(
          Text2,
          {
            style: {
              fontSize: item.prominent ? 13 : 10,
              fontWeight: item.prominent ? 700 : 400,
              marginBottom: 0,
              textAlign: "right"
            }
          },
          item.value
        )
      )
    )
  );
}

// packages/invoices/src/pdf.ts
function metadata(label, value, width) {
  return (0, import_react6.createElement)(
    View,
    { style: { ...width === void 0 ? { flex: 1 } : { width }, paddingRight: 12 } },
    (0, import_react6.createElement)(
      Text2,
      {
        style: {
          fontSize: 8,
          fontWeight: 700,
          color: theme.colors.mutedForeground,
          marginBottom: 4
        }
      },
      label
    ),
    (0, import_react6.createElement)(Text2, { style: { fontSize: 10, marginBottom: 0 } }, value)
  );
}
var treatmentLabels = {
  taxable: "10% GST",
  "gst-free": "GST-free",
  "input-taxed": "Input-taxed",
  "out-of-scope": "No GST"
};
function validatePdfInput(value) {
  const { id, sellerName, sellerAbn, state, logo, ...draftValue } = value;
  if (state !== "draft" && state !== "issued")
    throw new InvoiceValidationError("state: must be draft or issued");
  if (typeof sellerName !== "string" || sellerName.trim().length === 0 || !sellerName.isWellFormed() || Array.from(sellerName).length > 256 || Array.from(sellerName).some((character) => {
    const code = character.charCodeAt(0);
    return code < 32 && code !== 9 && code !== 10 && code !== 13 || code === 127;
  }))
    throw new InvoiceValidationError(
      "sellerName: is required and must be at most 256 characters without control characters"
    );
  const abn = validateAbn(sellerAbn, "sellerAbn", true);
  const draft = state === "issued" ? validateInvoiceForIssue(validateInvoiceDraft(draftValue), { name: sellerName, abn }) : validateInvoiceDraft(draftValue);
  return {
    ...draft,
    ...id === void 0 ? {} : { id },
    sellerName: sellerName.trim(),
    sellerAbn: abn,
    state,
    ...logo === void 0 ? {} : { logo: validateInvoicePdfLogo(logo) }
  };
}
async function renderInvoicePdf(value) {
  const data = validatePdfInput(value);
  assertInvoicePdfReady(data, data.sellerName, data.logo);
  const totals = calculateInvoice(data);
  const invoiceLabel = data.gstRegistered ? "Tax Invoice" : "Invoice";
  const title = `${data.state === "draft" ? "DRAFT \xB7 " : ""}${invoiceLabel}`;
  const rows = data.lines.map((item, index) => {
    const lineTotals = totals.lines[index];
    if (!lineTotals) throw new Error("Missing invoice line totals");
    return (0, import_react6.createElement)(
      TableRow,
      { key: `line-${index}`, stripe: index % 2 === 1 },
      (0, import_react6.createElement)(
        TableCell,
        { key: "description" },
        `${item.description}
${treatmentLabels[item.taxTreatment]}`
      ),
      (0, import_react6.createElement)(TableCell, { key: "quantity", align: "center" }, formatQuantity(item.quantityMilli)),
      (0, import_react6.createElement)(TableCell, { key: "price", align: "right" }, formatAud(item.unitPriceMinor)),
      (0, import_react6.createElement)(TableCell, { key: "gst", align: "right" }, formatAud(lineTotals.gstMinor)),
      (0, import_react6.createElement)(TableCell, { key: "amount", align: "right" }, formatAud(lineTotals.totalMinor))
    );
  });
  const document2 = (0, import_react6.createElement)(
    Document,
    {
      title: `${title} ${data.number || "unnumbered"}`,
      author: data.sellerName,
      creator: "Tammy \xB7 pdfcn / Forme",
      lang: "en-AU",
      tagged: true,
      style: { fontFamily: "Noto Sans", fontSize: 10, color: theme.colors.foreground }
    },
    (0, import_react6.createElement)(
      Page,
      { size: "A4", margin: { top: 40, left: 40, right: 40, bottom: 36 } },
      (0, import_react6.createElement)(PageFooter, {
        leftText: `${data.number || "Unnumbered draft"} \xB7 AUD${data.state === "draft" ? " \xB7 DRAFT \u2014 not issued" : ""}`
      }),
      data.state === "draft" ? (0, import_react6.createElement)(Watermark, { text: "DRAFT", fontSize: 72, color: "rgba(32,78,67,0.06)", angle: -35 }) : null,
      (0, import_react6.createElement)(PageHeader, {
        title,
        subtitle: data.sellerName,
        rightText: `ABN ${data.sellerAbn} \xB7 All amounts in Australian dollars (AUD)`,
        ...data.logo ? { logo: data.logo } : {}
      }),
      (0, import_react6.createElement)(
        View,
        { style: { flexDirection: "row", marginBottom: theme.spacing.sectionGap } },
        metadata("INVOICE NUMBER", data.number || "Unnumbered draft"),
        metadata("ISSUE DATE", data.issueDate || "To be completed"),
        metadata("DUE DATE", data.dueDate || "To be completed")
      ),
      (0, import_react6.createElement)(
        View,
        { style: { flexDirection: "row", marginBottom: theme.spacing.sectionGap } },
        metadata("FROM", `${data.sellerName}
${data.sellerAddress || "Address to be completed"}`),
        metadata(
          "BILLED TO",
          `${data.customerName || "Customer to be completed"}
${data.customerAddress || "Address to be completed"}${data.customerAbn ? `
ABN ${data.customerAbn}` : ""}`
        )
      ),
      (0, import_react6.createElement)(
        Table2,
        {},
        (0, import_react6.createElement)(
          TableRow,
          { key: "header", header: true },
          (0, import_react6.createElement)(TableCell, { key: "description", header: true }, "Description"),
          (0, import_react6.createElement)(TableCell, { key: "quantity", header: true, align: "center" }, "Qty"),
          (0, import_react6.createElement)(TableCell, { key: "price", header: true, align: "right" }, "Unit ex GST"),
          (0, import_react6.createElement)(TableCell, { key: "gst", header: true, align: "right" }, "GST"),
          (0, import_react6.createElement)(TableCell, { key: "amount", header: true, align: "right" }, "Amount incl GST")
        ),
        ...rows
      ),
      (0, import_react6.createElement)(
        View,
        {
          wrap: false,
          style: {
            flexDirection: "row",
            justifyContent: "flex-end",
            marginTop: 18,
            marginBottom: 20
          }
        },
        (0, import_react6.createElement)(KeyValue, {
          items: [
            { key: "Subtotal (ex GST)", value: formatAud(totals.subtotalMinor) },
            { key: "GST", value: formatAud(totals.gstMinor) },
            { key: "Total due (AUD)", value: formatAud(totals.totalMinor), prominent: true }
          ]
        })
      ),
      (0, import_react6.createElement)(
        Text2,
        { style: { fontSize: 8, color: theme.colors.mutedForeground } },
        data.gstRegistered ? "GST is calculated at 10% on taxable line amounts and rounded per line. Other tax treatments are shown alongside each item." : "Not registered for GST. No GST has been charged."
      ),
      data.paymentInstructions ? (0, import_react6.createElement)(
        View,
        { style: { marginTop: 16 } },
        (0, import_react6.createElement)(Text2, { style: { fontSize: 9, fontWeight: 700 } }, "PAYMENT INSTRUCTIONS"),
        (0, import_react6.createElement)(Text2, {}, data.paymentInstructions)
      ) : null,
      data.notes ? (0, import_react6.createElement)(
        View,
        { style: { marginTop: 14 } },
        (0, import_react6.createElement)(Text2, { style: { fontSize: 9, fontWeight: 700 } }, "NOTES"),
        (0, import_react6.createElement)(Text2, {}, data.notes)
      ) : null
    )
  );
  const bytes = await renderDocument(document2);
  if (data.logo && !/\/Subtype\s*\/Image\b/.test(new TextDecoder("latin1").decode(bytes)))
    throw new InvoiceValidationError("logo: image could not be rendered");
  return bytes;
}

// packages/invoices/test-support/pdf-text.ts
import { inflateSync } from "node:zlib";
function pdfStreams(bytes) {
  const buffer = Buffer.from(bytes);
  const source = buffer.toString("latin1");
  const parts = [source];
  const objects = /* @__PURE__ */ new Map();
  const streams = /* @__PURE__ */ new Map();
  for (const match of source.matchAll(/(?:^|\n)(\d+) 0 obj\n([\s\S]*?)\nendobj/g)) {
    const id = match[1];
    const body = match[2];
    if (!id || !body) continue;
    objects.set(id, body);
    const marker = /stream\r?\n/.exec(body);
    if (!marker) continue;
    const start = marker.index + marker[0].length;
    const end = body.lastIndexOf("endstream");
    if (end < start) continue;
    let decoded = body.slice(start, end);
    if (body.slice(0, marker.index).includes("/FlateDecode")) {
      try {
        decoded = inflateSync(Buffer.from(decoded, "latin1")).toString("latin1");
      } catch {
        continue;
      }
    }
    streams.set(id, decoded);
    parts.push(decoded);
  }
  const fonts = /* @__PURE__ */ new Map();
  for (const match of source.matchAll(/\/(F\d+)\s+(\d+)\s+0\s+R/g)) {
    const name = match[1];
    const id = match[2];
    if (!name || !id) continue;
    const unicodeId = objects.get(id)?.match(/\/ToUnicode\s+(\d+)\s+0\s+R/)?.[1];
    if (!unicodeId) continue;
    const map = /* @__PURE__ */ new Map();
    for (const block of (streams.get(unicodeId) ?? "").matchAll(
      /beginbfchar([\s\S]*?)endbfchar/g
    )) {
      for (const entry of (block[1] ?? "").matchAll(/<([\da-fA-F]+)>\s+<([\da-fA-F]+)>/g)) {
        if (entry[1] && entry[2])
          map.set(
            entry[1].toUpperCase(),
            Buffer.from(entry[2], "hex").swap16().toString("utf16le")
          );
      }
    }
    fonts.set(name, map);
  }
  for (const stream of streams.values()) {
    let activeFont = "F0";
    for (const item of stream.matchAll(/\/(F\d+)\s+[\d.]+\s+Tf|<([\da-fA-F]+)>\s+Tj/g)) {
      if (item[1]) activeFont = item[1];
      else if (item[2]) {
        const map = fonts.get(activeFont);
        parts.push(
          map ? (item[2].match(/.{4}/g) ?? []).map((code) => map.get(code.toUpperCase()) ?? "").join("") : Buffer.from(item[2], "hex").toString("latin1")
        );
      }
    }
  }
  return parts.join("\n");
}

// packages/invoices/test-support/logo-fixtures.ts
import { deflateSync } from "node:zlib";
function pngChunk(type, data) {
  const chunk = Buffer.alloc(data.length + 12);
  chunk.writeUInt32BE(data.length, 0);
  chunk.write(type, 4, 4, "ascii");
  chunk.set(data, 8);
  let crc = 4294967295;
  for (const byte of chunk.subarray(4, -4)) {
    crc ^= byte;
    for (let bit = 0; bit < 8; bit++) crc = crc >>> 1 ^ (crc & 1 ? 3988292384 : 0);
  }
  chunk.writeUInt32BE((crc ^ 4294967295) >>> 0, chunk.length - 4);
  return chunk;
}
function pngLogo(width = 240, height = 96) {
  const header = Buffer.alloc(13);
  header.writeUInt32BE(width, 0);
  header.writeUInt32BE(height, 4);
  header[8] = 8;
  header[9] = 6;
  const raster = Buffer.alloc(height * (width * 4 + 1));
  for (let y = 0; y < height; y++) {
    for (let x = 0; x < width; x++) {
      const offset = y * (width * 4 + 1) + 1 + x * 4;
      raster[offset] = 32;
      raster[offset + 1] = 78;
      raster[offset + 2] = 67;
      raster[offset + 3] = x >= 20 && x < 76 && y >= 20 && y < 32 || x >= 40 && x < 56 && y >= 20 && y < 76 || x >= 100 && x < 220 && y >= 28 && y < 42 || x >= 100 && x < 196 && y >= 56 && y < 68 ? 255 : 0;
    }
  }
  const bytes = Buffer.concat([
    Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]),
    pngChunk("IHDR", header),
    pngChunk("IDAT", deflateSync(raster)),
    pngChunk("IEND", new Uint8Array())
  ]);
  return { mimeType: "image/png", dataBase64: bytes.toString("base64"), width, height };
}

// ../../../output/tammy-invoice-demo/seed-demo.ts
var root = "/Users/benebsworth/projects/tammy/output/tammy-invoice-demo";
var profile = path.join(root, "user-data-company-template");
var executable = "/Users/benebsworth/projects/tammy/.worktrees/issue/sbr-registration-ready/.tmp/electron-builder/ordinary/mac-arm64/Tammy.app/Contents/MacOS/Tammy";
var username = "invoice-demo@example.test";
var password = "Tammy-demo-admin-2026!";
var passphrase = "Tammy-demo-workspace-2026!";
var sample = JSON.parse(await readFile("/Users/benebsworth/projects/tammy/output/pdf/example-invoice-2026-10-01.json", "utf8"));
var expenses = [
  { name: "Office stationery", supplier: "Example Stationery Pty Ltd", number: "SAMPLE-STATIONERY-001", filename: "01-office-stationery.pdf", subtotal: 8e3, gst: 800 },
  { name: "Courier delivery", supplier: "Example Courier Pty Ltd", number: "SAMPLE-COURIER-001", filename: "02-courier-delivery.pdf", subtotal: 2500, gst: 250 },
  { name: "Reference subscription", supplier: "Example Reference Library Pty Ltd", number: "SAMPLE-REFERENCE-001", filename: "03-reference-subscription.pdf", subtotal: 4500, gst: 0 }
];
var errors = [];
var stages = [];
var viewportChecks = [];
var application;
var applicationProcess;
var page;
function stage(message) {
  stages.push(message);
  console.log(message);
}
function uuidV7() {
  const bytes = randomBytes(16);
  let milliseconds = Date.now();
  for (let index = 5; index >= 0; index--) {
    bytes[index] = milliseconds & 255;
    milliseconds = Math.floor(milliseconds / 256);
  }
  bytes[6] = bytes[6] & 15 | 112;
  bytes[8] = bytes[8] & 63 | 128;
  const value = bytes.toString("hex");
  return `${value.slice(0, 8)}-${value.slice(8, 12)}-${value.slice(12, 16)}-${value.slice(16, 20)}-${value.slice(20)}`;
}
async function launch() {
  application = await _electron.launch({ executablePath: executable, args: [`--user-data-dir=${profile}`], chromiumSandbox: true, offline: true, timeout: 3e4 });
  applicationProcess = application.process();
  applicationProcess.stdout?.on("data", (value) => process.stdout.write(value));
  applicationProcess.stderr?.on("data", (value) => process.stderr.write(value));
  page = await application.firstWindow();
  page.setDefaultTimeout(15e3);
  page.on("pageerror", (error) => errors.push(error.message));
  page.on("console", (message) => {
    if (message.type() === "error") errors.push(message.text());
  });
  await page.waitForLoadState("domcontentloaded");
}
async function close() {
  if (!application) return;
  const owned = application;
  application = void 0;
  const process2 = applicationProcess;
  applicationProcess = void 0;
  if (!process2 || process2.exitCode !== null || process2.signalCode !== null) return;
  const exited = new Promise((resolve) => process2.once("close", () => resolve()));
  await owned.evaluate(({ app }) => {
    setTimeout(() => app.quit(), 250);
  });
  void owned.close().catch(() => void 0);
  let timer;
  try {
    await Promise.race([exited, new Promise((_, reject) => {
      timer = setTimeout(() => reject(new Error("DEMO_GRACEFUL_QUIT_TIMEOUT")), 2e4);
    })]);
  } finally {
    clearTimeout(timer);
  }
}
async function unlock() {
  await page.getByLabel("Workspace passphrase").fill(passphrase);
  await page.getByLabel("Email or username").fill(username);
  await page.getByLabel("Administrator password").fill(password);
  await page.getByRole("button", { name: "Unlock workspace", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Overview", exact: true })).toBeVisible();
}
async function verifySavedEditor() {
  const lineItems = page.getByRole("table", { name: "Invoice line items", exact: true });
  const reviewedExpenses = page.getByRole("table", { name: "Reviewed expenses", exact: true });
  await expect(lineItems).toBeVisible();
  await expect(lineItems.getByRole("row")).toHaveCount(4);
  await expect(lineItems.getByText("Linked reviewed expense \xB7 internal only", { exact: true })).toHaveCount(3);
  await expect(reviewedExpenses).toBeVisible();
  await expect(reviewedExpenses.getByRole("row")).toHaveCount(4);
  await expect(reviewedExpenses.getByText("Added", { exact: true })).toHaveCount(3);
  for (const expense of expenses) {
    const checkbox = reviewedExpenses.getByRole("checkbox", { name: `Select ${expense.supplier} \xB7 ${expense.number}`, exact: true });
    await expect(checkbox).toBeDisabled();
    await expect(checkbox).not.toBeChecked();
  }
  await expect(page.getByTestId("invoice-subtotal")).toContainText("$150.00");
  await expect(page.getByTestId("invoice-gst")).toContainText("$10.50");
  await expect(page.getByTestId("invoice-total")).toContainText("$160.50");
  await expect(page.getByTestId("invoice-cost")).toContainText("$150.00");
  await expect(page.getByTestId("invoice-margin")).toContainText("$0.00");
  await expect(page.getByTestId("invoice-margin")).toContainText("Profit (ex GST)");
}
async function captureEditorViewport(width, height, filename) {
  await application.evaluate(({ BrowserWindow }, dimensions2) => {
    BrowserWindow.getAllWindows()[0].setContentSize(dimensions2.width, dimensions2.height);
  }, { width, height });
  await expect.poll(() => page.evaluate(() => ({ width: innerWidth, height: innerHeight }))).toEqual({ width, height });
  const dimensions = await page.evaluate(() => {
    const lineTable = document.querySelector('table[aria-label="Invoice line items"]');
    const scrollContainer = lineTable.parentElement;
    return { width: innerWidth, height: innerHeight, pageScrollWidth: document.documentElement.scrollWidth, pageClientWidth: document.documentElement.clientWidth, tableScrollWidth: scrollContainer.scrollWidth, tableClientWidth: scrollContainer.clientWidth };
  });
  assert2.ok(dimensions.pageScrollWidth <= dimensions.pageClientWidth, `Page overflow at ${width}\xD7${height}: ${JSON.stringify(dimensions)}`);
  viewportChecks.push(dimensions);
  await page.screenshot({ path: path.join(root, filename), fullPage: true });
  stage(`Captured ${width}\xD7${height} saved editor; page horizontal overflow absent, table scroll ${dimensions.tableScrollWidth}/${dimensions.tableClientWidth}.`);
}
async function authority() {
  const workspace = await page.evaluate(() => {
    const serialized = sessionStorage.getItem("tammy.session.active");
    if (!serialized) throw new Error("DEMO_SESSION_MISSING");
    return JSON.parse(serialized);
  });
  return { authentication: { actorUserId: workspace.userId, sessionId: workspace.sessionId }, organisationId: workspace.organisationId };
}
async function rpc(module, method, input, output, value) {
  const request = toBinary(AnySchema, create(AnySchema, { typeUrl: `type.googleapis.com/${input.typeName}`, value: toBinary(input, create(input, value)) }));
  const response = await page.evaluate(async ({ module: module2, method: method2, request: request2 }) => {
    const envelope2 = await window.conveyor.invoke(`conveyor:${module2}`, method2, Uint8Array.from(request2));
    if (!envelope2.ok) throw new Error(`${method2}:${envelope2.error?.code ?? "UNKNOWN"}`);
    return Array.from(envelope2.data);
  }, { module, method, request: Array.from(request) });
  const envelope = fromBinary(AnySchema, Uint8Array.from(response));
  assert2.equal(envelope.typeUrl, `type.googleapis.com/${output.typeName}`);
  return fromBinary(output, envelope.value);
}
var money = (minorUnits) => ({ currencyCode: "AUD", minorUnits: BigInt(minorUnits) });
var command = (authentication) => ({ authentication, idempotencyKey: uuidV7() });
var listDocuments = (context) => rpc("documents", "listDocuments", ListDocumentsRequestSchema, ListDocumentsResponseSchema, { ...context, page: { pageSize: 100 } });
var listInvoices = (context) => rpc("accounting", "listInvoices", ListInvoicesRequestSchema, ListInvoicesResponseSchema, { ...context, page: { pageSize: 100 } });
await mkdir(path.join(root, "sample-expenses"), { recursive: true });
await writeFile(path.join(root, "README.md"), `# Tammy invoice demo

This is an isolated test workspace containing fictional data. No invoice has been issued.

## Open the demo

Run:

\`\`\`sh
rtk proxy "${executable}" --user-data-dir="${profile}"
\`\`\`

You can also run \`rtk proxy zsh /Users/benebsworth/projects/tammy/output/tammy-invoice-demo/open-demo.sh\` to open a separate macOS app instance. This helper should be used only when the demo is closed.

The packaged app may be rebuilt at the same location. Keep the profile directory to retain this demo.

## Test-only sign-in

- Email or username: \`${username}\`
- Administrator password: \`${password}\`
- Workspace passphrase: \`${passphrase}\`

These credentials are intentionally stored here for this fictional demo only.

## What to inspect

1. Open **Documents** for three reviewed sample expenses. Their source PDFs are also in \`sample-expenses/\`.
2. Open **Invoices**, then **Open TEST-2026-001**. The saved draft links one row in **Invoice line items** to each reviewed expense. The **Reviewed expenses** table marks all three sources **Added** and prevents duplicate selection.
3. Verify $150.00 before GST, $10.50 GST and $160.50 total. Internal cost is $150.00 and profit before GST is $0.00.
4. Use **Export PDF** to save another draft copy. The seed run exports \`TEST-2026-001-exported-from-Tammy.pdf\`.

All names, addresses, identifiers and expense tax treatments are examples chosen for testing. No payment is required.

## Rerun seed verification

Run \`rtk proxy zsh /Users/benebsworth/projects/tammy/output/tammy-invoice-demo/run-seed.sh\`. It preserves this profile, reuses matching sample expenses and saves the same draft. It must not run while this demo is open. The script closes the app when finished.
`);
try {
  await launch();
  await page.waitForFunction(() => document.querySelector("h1")?.textContent?.trim());
  const heading = await page.locator("h1").first().textContent();
  if (heading?.includes("Unlock")) {
    await unlock();
    stage("Unlocked existing isolated demo workspace.");
  } else {
    await page.evaluate(() => {
      history.replaceState(null, "", "/setup/workspace");
      dispatchEvent(new PopStateEvent("popstate"));
    });
    await expect(page.getByRole("heading", { name: "Create your local workspace" })).toBeVisible();
    await page.getByLabel("Your name").fill("Invoice Demo");
    await page.getByLabel("Email or username").fill(username);
    await page.getByLabel("Business legal name").fill(sample.sellerName);
    await page.getByLabel("Business display name").fill("Wattle & Co Test");
    await page.getByLabel("ABN", { exact: true }).fill(sample.sellerAbn);
    await page.getByLabel("Workspace passphrase").fill(passphrase);
    await page.getByLabel("Administrator password").fill(password);
    await page.getByRole("button", { name: "Create local workspace", exact: true }).click();
    await expect(page.getByRole("heading", { name: "Save your recovery code" })).toBeVisible();
    await page.getByRole("button", { name: "I saved my recovery code" }).click();
    await expect(page.getByRole("heading", { name: "Overview", exact: true })).toBeVisible();
    stage("Created isolated fictional demo workspace.");
  }
  let context = await authority();
  const existingDocuments = await listDocuments(context);
  const documentIds = [];
  for (const [index, expense] of expenses.entries()) {
    const sourceDisplayName = `${expense.name} - example expense.pdf`;
    let document2 = existingDocuments.documents.find((value) => value.sourceDisplayName === sourceDisplayName);
    const candidate = { supplierName: expense.supplier, invoiceNumber: expense.number, documentDate: { year: 2026, month: 10, day: 1 }, subtotal: money(expense.subtotal), gst: money(expense.gst), total: money(expense.subtotal + expense.gst) };
    const original = await renderInvoicePdf({ ...sample, number: expense.number, sellerName: expense.supplier, sellerAbn: "11000000560", sellerAddress: `${index + 10} Example Supplier Road
Melbourne VIC 3000`, customerName: sample.sellerName, customerAddress: sample.sellerAddress, lines: [{ description: expense.name, quantityMilli: 1e3, unitPriceMinor: expense.subtotal, taxTreatment: expense.gst ? "taxable" : "gst-free" }], notes: "Synthetic source expense for testing only. Amounts and tax treatment are examples.", paymentInstructions: "Example expense. No payment required.", state: "draft" });
    await writeFile(path.join(root, "sample-expenses", expense.filename), original);
    if (!document2) {
      document2 = (await rpc("documents", "ingestDocument", IngestDocumentRequestSchema, IngestDocumentResponseSchema, { commandContext: command(context.authentication), organisationId: context.organisationId, sourceDisplayName, mimeType: "application/pdf", original, extractedText: `${expense.supplier}
${expense.number}
${expense.name}
Subtotal AUD ${(expense.subtotal / 100).toFixed(2)}
GST AUD ${(expense.gst / 100).toFixed(2)}
Total AUD ${((expense.subtotal + expense.gst) / 100).toFixed(2)}
Example data for testing only`, candidate })).document;
    }
    if (document2.status !== 2 /* REVIEWED */) {
      document2 = (await rpc("documents", "saveDocumentReview", SaveDocumentReviewRequestSchema, SaveDocumentReviewResponseSchema, { commandContext: command(context.authentication), documentId: document2.id, expectedVersion: document2.version, candidate })).document;
    }
    assert2.equal(document2.status, 2 /* REVIEWED */);
    assert2.equal(document2.candidate.subtotal.minorUnits, BigInt(expense.subtotal));
    assert2.equal(document2.candidate.gst.minorUnits, BigInt(expense.gst));
    documentIds.push(document2.id);
    stage(`Retained and reviewed ${expense.name}: AUD ${((expense.subtotal + expense.gst) / 100).toFixed(2)}.`);
  }
  const { sellerName, sellerAbn, state, ...draft } = sample;
  draft.lines = draft.lines.map((line2, index) => ({ ...line2, sourceDocumentId: documentIds[index] }));
  const invoices = await listInvoices(context);
  const existing = invoices.invoices.find((value) => value.draft?.number === sample.number);
  if (existing) assert2.equal(existing.state, 1 /* DRAFT */, "Never overwrite an issued invoice");
  const saved = (await rpc("accounting", "saveInvoiceDraft", SaveInvoiceDraftRequestSchema, SaveInvoiceDraftResponseSchema, { commandContext: command(context.authentication), organisationId: context.organisationId, invoiceId: existing?.id ?? "", expectedVersion: existing?.version ?? 0n, draft: invoiceDraftToProto(draft) })).invoice;
  assert2.equal(saved.state, 1 /* DRAFT */);
  assert2.equal(saved.totalMinor, 16050n);
  await page.getByRole("link", { name: "Invoices", exact: true }).click();
  await page.getByRole("button", { name: "Open TEST-2026-001", exact: true }).click();
  await expect(page.getByLabel("Customer name", { exact: true })).toHaveValue(sample.customerName);
  await verifySavedEditor();
  await page.getByRole("button", { name: "Manage invoice template", exact: true }).click();
  await page.getByRole("textbox", { name: "Template seller address", exact: true }).fill(sample.sellerAddress);
  await page.getByRole("textbox", { name: "Default notes", exact: true }).fill(sample.notes);
  await page.getByRole("textbox", { name: "Default payment instructions", exact: true }).fill(sample.paymentInstructions);
  await page.getByLabel("Payment terms (days)", { exact: true }).fill("14");
  const logo = pngLogo();
  await writeFile(path.join(root, "sample-company-logo.png"), Buffer.from(logo.dataBase64, "base64"));
  if (await page.getByText("No company logo selected.", { exact: true }).isVisible()) {
    await page.getByLabel("Company logo", { exact: true }).setInputFiles(path.join(root, "sample-company-logo.png"));
    await expect(page.getByRole("img", { name: "Company logo preview", exact: true })).toBeVisible();
  }
  await page.getByRole("button", { name: "Save template", exact: true }).click();
  await expect(page.getByText("Company template saved.", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Apply saved template", exact: true }).click();
  await expect(page.getByRole("img", { name: "Invoice logo", exact: true })).toBeVisible();
  await captureEditorViewport(1440, 900, "company-template.png");
  await page.getByRole("button", { name: "Close template editor", exact: true }).click();
  await page.getByRole("button", { name: "Save draft", exact: true }).click();
  await expect(page.getByText("Draft saved.", { exact: true })).toBeVisible();
  const exportedPdf = path.join(root, "TEST-2026-001-exported-from-Tammy.pdf");
  await application.evaluate(({ dialog }, filePath) => {
    dialog.showSaveDialog = async () => ({ canceled: false, filePath });
  }, exportedPdf);
  await page.getByRole("button", { name: "Export PDF", exact: true }).click();
  await expect(page.getByText("Invoice PDF saved.", { exact: true })).toBeVisible();
  const exportedBytes = await readFile(exportedPdf);
  assert2.match(exportedBytes.toString("latin1"), /\/Subtype\s*\/Image\b/);
  const exported = pdfStreams(exportedBytes);
  for (const expected of ["DRAFT", sample.number, sample.customerName, "150.00", "10.50", "160.50", "Page 1 of 1"]) assert2.ok(exported.includes(expected), `PDF missing ${expected}`);
  for (const id of documentIds) assert2.ok(!exported.includes(id));
  assert2.doesNotMatch(exported, /unitCostMinor|marginMinor|sourceDocumentId|Internal cost|Margin|Profit/);
  assert2.ok(exported.includes(sample.paymentInstructions));
  stage("Saved the linked draft through Tammy and exported its real bundled-engine PDF.");
  await close();
  await launch();
  await unlock();
  context = await authority();
  const reopenedDocuments = (await listDocuments(context)).documents;
  assert2.equal(reopenedDocuments.length, 3);
  assert2.ok(reopenedDocuments.every((value) => value.status === 2 /* REVIEWED */));
  const reopened = (await listInvoices(context)).invoices.find((value) => value.id === saved.id);
  assert2.equal(reopened.state, 1 /* DRAFT */);
  assert2.equal(reopened.subtotalMinor, 15000n);
  assert2.equal(reopened.gstMinor, 1050n);
  assert2.equal(reopened.totalMinor, 16050n);
  assert2.deepEqual(reopened.draft.lines.map((line2) => line2.sourceDocumentId), documentIds);
  await page.getByRole("link", { name: "Documents", exact: true }).click();
  for (const expense of expenses) {
    await page.getByRole("button", { name: new RegExp(`${expense.supplier} ${expense.number}`) }).click();
    await expect(page.getByRole("heading", { name: `${expense.name} - example expense.pdf`, exact: true })).toBeVisible();
    await expect(page.getByLabel("Supplier", { exact: true })).toHaveValue(expense.supplier);
    await expect(page.getByRole("button", { name: "Review saved", exact: true })).toBeDisabled();
  }
  await page.screenshot({ path: path.join(root, "reviewed-expenses.png"), fullPage: true });
  await page.getByRole("link", { name: "Invoices", exact: true }).click();
  await page.getByRole("button", { name: "Open TEST-2026-001", exact: true }).click();
  await expect(page.getByLabel("Customer name", { exact: true })).toHaveValue(sample.customerName);
  await expect(page.getByLabel("Customer name", { exact: true })).toBeEnabled();
  await verifySavedEditor();
  await captureEditorViewport(1440, 900, "saved-draft.png");
  await captureEditorViewport(1024, 700, "saved-draft-1024x700.png");
  assert2.deepEqual(errors, []);
  stage("Restart verified: all three expenses remain reviewed and TEST-2026-001 remains an editable saved draft.");
  await expect(page.getByRole("img", { name: "Invoice logo", exact: true })).toBeVisible();
  await writeFile(path.join(root, "verification.json"), `${JSON.stringify({ verifiedAt: (/* @__PURE__ */ new Date()).toISOString(), profile, executable, invoiceNumber: sample.number, invoiceId: saved.id, state: "draft", sourceDocumentIds: documentIds, subtotalMinor: 15e3, gstMinor: 1050, totalMinor: 16050, expenseReviewPersisted: true, draftReopened: true, companyTemplateSaved: true, invoiceLogoPersisted: true, invoicePdfLogoEmbedded: true, semanticTablesVerified: true, linkedExpenseReselectionDisabled: true, viewportChecks, exportedPdf, consoleErrors: errors, stages }, null, 2)}
`);
  await writeFile(path.join(root, "README.md"), `${await readFile(path.join(root, "README.md"), "utf8")}
## Company invoice template

The saved draft includes a sample company logo. Use **Manage invoice template** on the invoice page to change the logo, seller address, notes, payment instructions and payment terms. New invoices inherit saved defaults; **Apply saved template** updates only the editable invoice in view. The sample payment instructions are fictional and no payment is required.
`);
  await rm(path.join(root, "seed-failure.txt"), { force: true });
} catch (error) {
  stage(`FAILED: ${error instanceof Error ? error.message : String(error)}`);
  await writeFile(path.join(root, "seed-failure.txt"), stages.join("\n"));
  throw error;
} finally {
  await close();
}
stage("Closed Tammy gracefully. Persistent demo profile is ready to reopen.");
/*! Bundled license information:

react/cjs/react.production.js:
  (**
   * @license React
   * react.production.js
   *
   * Copyright (c) Meta Platforms, Inc. and affiliates.
   *
   * This source code is licensed under the MIT license found in the
   * LICENSE file in the root directory of this source tree.
   *)

react/cjs/react.development.js:
  (**
   * @license React
   * react.development.js
   *
   * Copyright (c) Meta Platforms, Inc. and affiliates.
   *
   * This source code is licensed under the MIT license found in the
   * LICENSE file in the root directory of this source tree.
   *)
*/
