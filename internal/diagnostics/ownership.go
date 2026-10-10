package diagnostics

// Diagnostics of the "own and shared threads" extension: the ownership (region) analysis and the
// `ownRuntime` option.
//
// These are hand-written rather than generated: diagnostics_generated.go is produced from the
// TypeScript submodule's diagnosticMessages.json, which knows nothing about the extension. The codes
// live in a private range (96000-96099) that TypeScript does not use.

var X_0_cannot_be_used_here_Colon_it_was_sent_to_1_on_line_2 = &Message{code: 96001, category: CategoryError, key: "_0_cannot_be_used_here_Colon_it_was_sent_to_1_on_line_2_96001", text: "'{0}' cannot be used here: it was sent to '{1}' on line {2}."}

var X_0_cannot_be_used_here_Colon_it_may_have_been_sent_to_1_on_line_2 = &Message{code: 96002, category: CategoryError, key: "_0_cannot_be_used_here_Colon_it_may_have_been_sent_to_1_on_line_2_96002", text: "'{0}' cannot be used here: it may have been sent to '{1}' on line {2}."}

var X_0_cannot_be_used_here_Colon_it_shares_objects_with_1_which_was_sent_to_2_on_line_3 = &Message{code: 96003, category: CategoryError, key: "_0_cannot_be_used_here_Colon_it_shares_objects_with_1_which_was_sent_to_2_on_line_3_96003", text: "'{0}' cannot be used here: it shares objects with '{1}', which was sent to '{2}' on line {3}."}

var X_0_cannot_be_used_here_Colon_it_shares_objects_with_1_which_may_have_been_sent_to_2_on_line_3 = &Message{code: 96004, category: CategoryError, key: "_0_cannot_be_used_here_Colon_it_shares_objects_with_1_which_may_have_been_sent_to_2_on_line_3_96004", text: "'{0}' cannot be used here: it shares objects with '{1}', which may have been sent to '{2}' on line {3}."}

var X_0_was_consumed_by_1_on_line_2 = &Message{code: 96005, category: CategoryError, key: "_0_was_consumed_by_1_on_line_2_96005", text: "'{0}' was consumed by '{1}' on line {2}."}

var X_0_may_have_been_consumed_by_1_on_line_2 = &Message{code: 96006, category: CategoryError, key: "_0_may_have_been_consumed_by_1_on_line_2_96006", text: "'{0}' may have been consumed by '{1}' on line {2}."}

var X_0_cannot_be_sent_Colon_it_may_be_referenced_by_the_caller_Declare_the_parameter_sending_to_take_ownership = &Message{code: 96007, category: CategoryError, key: "_0_cannot_be_sent_Colon_it_may_be_referenced_by_the_caller_Declare_the_parameter_sending_to_take_owne_96007", text: "'{0}' cannot be sent: it may be referenced by the caller. Declare the parameter 'sending' to take ownership."}

var X_0_cannot_be_sent_Colon_it_is_captured_from_an_outer_scope = &Message{code: 96008, category: CategoryError, key: "_0_cannot_be_sent_Colon_it_is_captured_from_an_outer_scope_96008", text: "'{0}' cannot be sent: it is captured from an outer scope."}

var X_0_cannot_be_returned_as_sending_Colon_it_may_be_referenced_by_the_caller = &Message{code: 96009, category: CategoryError, key: "_0_cannot_be_returned_as_sending_Colon_it_may_be_referenced_by_the_caller_96009", text: "'{0}' cannot be returned as 'sending': it may be referenced by the caller."}

var Cannot_change_0_Colon_it_is_a_shared_value_which_is_immutable = &Message{code: 96010, category: CategoryError, key: "Cannot_change_0_Colon_it_is_a_shared_value_which_is_immutable_96010", text: "Cannot change '{0}': it is a shared value, which is immutable."}

var X_0_is_sent_here = &Message{code: 96011, category: CategoryMessage, key: "_0_is_sent_here_96011", text: "'{0}' is sent here."}

var X_0_is_consumed_here = &Message{code: 96012, category: CategoryMessage, key: "_0_is_consumed_here_96012", text: "'{0}' is consumed here."}

var Specify_the_module_that_files_using_the_own_Slashshared_threads_syntax_import_their_runtime_helpers_from_e_g_Dot_Slashown_mjs = &Message{code: 96013, category: CategoryMessage, key: "Specify_the_module_that_files_using_the_own_Slashshared_threads_syntax_import_their_runtime_helpers_96013", text: "Specify the module that files using the own/shared threads syntax import their runtime helpers from, e.g. './own.mjs'."}

var Option_0_can_only_be_used_when_module_is_set_to_es2015_or_later = &Message{code: 96014, category: CategoryError, key: "Option_0_can_only_be_used_when_module_is_set_to_es2015_or_later_96014", text: "Option '{0}' can only be used when 'module' is set to 'es2015' or later."}

// Looks up an extension diagnostic by key, for diagnostics that are re-localized from their key
// (keyToMessage only knows the generated messages).
func ownershipKeyToMessage(key Key) *Message {
	for _, m := range []*Message{
		X_0_cannot_be_used_here_Colon_it_was_sent_to_1_on_line_2,
		X_0_cannot_be_used_here_Colon_it_may_have_been_sent_to_1_on_line_2,
		X_0_cannot_be_used_here_Colon_it_shares_objects_with_1_which_was_sent_to_2_on_line_3,
		X_0_cannot_be_used_here_Colon_it_shares_objects_with_1_which_may_have_been_sent_to_2_on_line_3,
		X_0_was_consumed_by_1_on_line_2,
		X_0_may_have_been_consumed_by_1_on_line_2,
		X_0_cannot_be_sent_Colon_it_may_be_referenced_by_the_caller_Declare_the_parameter_sending_to_take_ownership,
		X_0_cannot_be_sent_Colon_it_is_captured_from_an_outer_scope,
		X_0_cannot_be_returned_as_sending_Colon_it_may_be_referenced_by_the_caller,
		Cannot_change_0_Colon_it_is_a_shared_value_which_is_immutable,
		X_0_is_sent_here,
		X_0_is_consumed_here,
		Specify_the_module_that_files_using_the_own_Slashshared_threads_syntax_import_their_runtime_helpers_from_e_g_Dot_Slashown_mjs,
		Option_0_can_only_be_used_when_module_is_set_to_es2015_or_later,
	} {
		if m.key == key {
			return m
		}
	}
	return nil
}
