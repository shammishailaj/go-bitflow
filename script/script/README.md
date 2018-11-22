# Bitflow Script Antlr implementation

## Implementation approach

### Overview

The Listener pattern of ANTLR is used, not the Visitor pattern, because the Golang version of the visitor pattern is not
functioning yet. [See Github](https://github.com/antlr/antlr4/pull/1807)
 
- **internal**: contains the code generated by ANTLR.
- **processor_registry**: the registry for the available functions of this agent
- **agent_parser**: the parser implementation, takes a bitflow script and returns an executable pipeline
- **generic_stackmap**: used for agent parser, see below

### State and Stackmap

The agent_parser implements the antlr listener of the bitflow script grammar.
StackMap 'state' contains the state of the pipeline while building it, it is a map from stringKey->Stack<interface{}>.
If state needs to be stored, it is pushed onto the stack of the appropriate key and popped when not used anymore.
This works well, because the listener moves through the AST using a DEPTH FIRST algorithm. Therefore nested Forks, Windows, etc. naturally work
until the physical memory is full.

Example with a fork:
```
   Enter Fork					-   
	    EnterSubPipeline:	 	Push a new subpipeline on the *'pipeline'* stack.   
		      EnterTransform:	-   
		      ExitTransform:	Add transform to the pipeline on top of stack, which is the pushed subpipeline   
	    ExitSubPipeline:	 	pop last subpipeline from stack and add it to *'fork_subpipelines'*   
	    EnterSubPipeline:	 	Push a new subpipeline on the *'pipeline'* stack.   
		      ... create subpipe
	    ExitSubPipeline:	 	pop last subpipeline from stack and add it to *'fork_subpipelines'*
    Exit Fork                   create Fork with all subpipelines from *'fork_subpipelines'*
```