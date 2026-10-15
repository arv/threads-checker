package diagnostics

// Diagnostics of the "own and shared threads" extension: the ownership (region) analysis, the
// `ownRuntime` option and the typed layouts of shared structs and SharedArrays.
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

var X_0_cannot_be_sent_Colon_it_may_be_referenced_by_the_caller_Declare_the_parameter_Sending_T_to_take_ownership = &Message{code: 96007, category: CategoryError, key: "_0_cannot_be_sent_Colon_it_may_be_referenced_by_the_caller_Declare_the_parameter_Sending_T_to_take_o_96007", text: "'{0}' cannot be sent: it may be referenced by the caller. Declare the parameter 'Sending<T>' to take ownership."}

var X_0_cannot_be_sent_Colon_it_is_captured_from_an_outer_scope = &Message{code: 96008, category: CategoryError, key: "_0_cannot_be_sent_Colon_it_is_captured_from_an_outer_scope_96008", text: "'{0}' cannot be sent: it is captured from an outer scope."}

var X_0_cannot_be_returned_as_Sending_T_Colon_it_may_be_referenced_by_the_caller = &Message{code: 96009, category: CategoryError, key: "_0_cannot_be_returned_as_Sending_T_Colon_it_may_be_referenced_by_the_caller_96009", text: "'{0}' cannot be returned as 'Sending<T>': it may be referenced by the caller."}

var Cannot_change_0_Colon_it_is_a_shared_value_which_is_immutable = &Message{code: 96010, category: CategoryError, key: "Cannot_change_0_Colon_it_is_a_shared_value_which_is_immutable_96010", text: "Cannot change '{0}': it is a shared value, which is immutable."}

var X_0_is_sent_here = &Message{code: 96011, category: CategoryMessage, key: "_0_is_sent_here_96011", text: "'{0}' is sent here."}

var X_0_is_consumed_here = &Message{code: 96012, category: CategoryMessage, key: "_0_is_consumed_here_96012", text: "'{0}' is consumed here."}

var Specify_the_own_Slashshared_threads_runtime_module_e_g_arv_Slashthreads = &Message{code: 96013, category: CategoryMessage, key: "Specify_the_own_Slashshared_threads_runtime_module_e_g_arv_Slashthreads_96013", text: "Specify the own/shared threads runtime module, e.g. '@arv/threads'."}

var Option_0_can_only_be_used_when_module_is_set_to_es2015_or_later = &Message{code: 96014, category: CategoryError, key: "Option_0_can_only_be_used_when_module_is_set_to_es2015_or_later_96014", text: "Option '{0}' can only be used when 'module' is set to 'es2015' or later."}

var A_shared_struct_field_must_be_a_number_a_boolean_a_string_a_shared_struct_a_SharedArray_or_a_Channel = &Message{code: 96015, category: CategoryError, key: "A_shared_struct_field_must_be_a_number_a_boolean_a_string_a_shared_struct_a_SharedArray_or_a_Channel_96015", text: "A shared struct field must be a number, a boolean, a string, a shared struct, a SharedArray or a Channel."}

var The_elements_of_a_SharedArray_must_be_numbers_booleans_strings_or_a_shared_struct = &Message{code: 96016, category: CategoryError, key: "The_elements_of_a_SharedArray_must_be_numbers_booleans_strings_or_a_shared_struct_96016", text: "The elements of a SharedArray must be numbers, booleans, strings or a shared struct."}

var The_elements_of_a_SharedArray_of_shared_structs_are_stored_inline_Colon_assign_their_fields_instead = &Message{code: 96017, category: CategoryError, key: "The_elements_of_a_SharedArray_of_shared_structs_are_stored_inline_Colon_assign_their_fields_instead_96017", text: "The elements of a SharedArray of shared structs are stored inline: assign their fields instead."}

var Optional_chaining_is_not_supported_on_0_Colon_its_value_is_an_address_in_the_shared_heap = &Message{code: 96018, category: CategoryError, key: "Optional_chaining_is_not_supported_on_0_Colon_its_value_is_an_address_in_the_shared_heap_96018", text: "Optional chaining is not supported on '{0}': its value is an address in the shared heap."}

var X_0_cannot_be_destructured_here_Colon_its_value_is_an_address_in_the_shared_heap = &Message{code: 96019, category: CategoryError, key: "_0_cannot_be_destructured_here_Colon_its_value_is_an_address_in_the_shared_heap_96019", text: "'{0}' cannot be destructured here: its value is an address in the shared heap."}

var X_0_cannot_be_reassigned_Colon_a_closure_captures_it_and_its_value_is_a_counted_address_in_the_shared_heap = &Message{code: 96020, category: CategoryError, key: "_0_cannot_be_reassigned_Colon_a_closure_captures_it_and_its_value_is_a_counted_address_in_the_shared_96020", text: "'{0}' cannot be reassigned: a closure captures it, and its value is a counted address in the shared heap."}

var A_value_of_type_0_cannot_be_stored_in_an_ordinary_array_or_object_Colon_reference_counting_cannot_see_it_there_Use_a_SharedArray_or_a_shared_struct_field_or_a_place_typed_unknown_which_holds_a_handle = &Message{code: 96021, category: CategoryError, key: "A_value_of_type_0_cannot_be_stored_in_an_ordinary_array_or_object_Colon_reference_counting_cannot_se_96021", text: "A value of type '{0}' cannot be stored in an ordinary array or object: reference counting cannot see it there. Use a SharedArray or a shared struct field, or a place typed 'unknown', which holds a handle."}

var A_for_of_or_for_in_variable_cannot_have_type_0_Colon_its_value_is_an_address_in_the_shared_heap = &Message{code: 96022, category: CategoryError, key: "A_for_of_or_for_in_variable_cannot_have_type_0_Colon_its_value_is_an_address_in_the_shared_heap_96022", text: "A 'for-of' or 'for-in' variable cannot have type '{0}': its value is an address in the shared heap."}

var X_0_cannot_be_captured_by_a_method_or_a_class_Colon_its_value_is_a_counted_address_in_the_shared_heap_which_only_functions_and_arrow_functions_can_hold = &Message{code: 96023, category: CategoryError, key: "_0_cannot_be_captured_by_a_method_or_a_class_Colon_its_value_is_a_counted_address_in_the_shared_heap_96023", text: "'{0}' cannot be captured by a method or a class: its value is a counted address in the shared heap, which only functions and arrow functions can hold."}

var X_0_cannot_be_bound_by_destructuring_Colon_its_value_is_an_address_in_the_shared_heap = &Message{code: 96024, category: CategoryError, key: "_0_cannot_be_bound_by_destructuring_Colon_its_value_is_an_address_in_the_shared_heap_96024", text: "'{0}' cannot be bound by destructuring: its value is an address in the shared heap."}

var A_generator_s_parameter_cannot_have_type_0_Colon_the_generator_runs_after_its_caller_s_borrow_ends = &Message{code: 96025, category: CategoryError, key: "A_generator_s_parameter_cannot_have_type_0_Colon_the_generator_runs_after_its_caller_s_borrow_ends_96025", text: "A generator's parameter cannot have type '{0}': the generator runs after its caller's borrow ends."}

var Field_0_is_at_different_places_in_the_shared_structs_of_this_union = &Message{code: 96026, category: CategoryError, key: "Field_0_is_at_different_places_in_the_shared_structs_of_this_union_96026", text: "Field '{0}' is at different places in the shared structs of this union."}

var An_atomic_operation_takes_an_i32_field_of_a_shared_struct_named_by_a_string_literal_or_an_element_of_a_SharedArray_i32 = &Message{code: 96028, category: CategoryError, key: "An_atomic_operation_takes_an_i32_field_of_a_shared_struct_named_by_a_string_literal_or_an_element_of_a_SharedArray_i32_96028", text: "An atomic operation takes an i32 field of a shared struct, named by a string literal, or an element of a SharedArray<i32>."}

var X_0_blocks_its_thread_and_with_it_every_async_function_waiting_on_the_same_worker_Colon_in_an_async_function_use_1 = &Message{code: 96030, category: CategoryError, key: "X_0_blocks_its_thread_and_with_it_every_async_function_waiting_on_the_same_worker_Colon_in_an_async_function_use_1_96030", text: "'{0}' blocks its thread, and with it every async function waiting on the same worker: in an async function, use '{1}'."}

var X_0_blocks_its_thread_and_with_it_every_async_function_waiting_on_the_same_worker_which_an_async_function_must_not_do = &Message{code: 96031, category: CategoryError, key: "X_0_blocks_its_thread_and_with_it_every_async_function_waiting_on_the_same_worker_which_an_async_function_must_not_do_96031", text: "'{0}' blocks its thread, and with it every async function waiting on the same worker, which an async function must not do."}

var This_value_may_not_be_an_integer_which_0_would_truncate_silently_Colon_convert_it_explicitly_as_with_Math_round_Math_trunc_or_0 = &Message{code: 96029, category: CategoryError, key: "This_value_may_not_be_an_integer_which_0_would_truncate_silently_Colon_convert_it_explicitly_as_with_Math_round_Math_trunc_or_0_96029", text: "This value may not be an integer, which {0} would truncate silently: convert it explicitly, as with Math.round(), Math.trunc() or | 0."}

var A_string_or_a_channel_in_a_shared_struct_or_SharedArray_cannot_be_assigned_by_destructuring_or_as_a_loop_variable = &Message{code: 96027, category: CategoryError, key: "A_string_or_a_channel_in_a_shared_struct_or_SharedArray_cannot_be_assigned_by_destructuring_or_as_a_loop_variable_96027", text: "A string or a channel in a shared struct or SharedArray cannot be assigned by destructuring or as a loop variable."}

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
		X_0_cannot_be_sent_Colon_it_may_be_referenced_by_the_caller_Declare_the_parameter_Sending_T_to_take_ownership,
		X_0_cannot_be_sent_Colon_it_is_captured_from_an_outer_scope,
		X_0_cannot_be_returned_as_Sending_T_Colon_it_may_be_referenced_by_the_caller,
		Cannot_change_0_Colon_it_is_a_shared_value_which_is_immutable,
		X_0_is_sent_here,
		X_0_is_consumed_here,
		Specify_the_own_Slashshared_threads_runtime_module_e_g_arv_Slashthreads,
		Option_0_can_only_be_used_when_module_is_set_to_es2015_or_later,
		A_shared_struct_field_must_be_a_number_a_boolean_a_string_a_shared_struct_a_SharedArray_or_a_Channel,
		The_elements_of_a_SharedArray_must_be_numbers_booleans_strings_or_a_shared_struct,
		The_elements_of_a_SharedArray_of_shared_structs_are_stored_inline_Colon_assign_their_fields_instead,
		Optional_chaining_is_not_supported_on_0_Colon_its_value_is_an_address_in_the_shared_heap,
		X_0_cannot_be_destructured_here_Colon_its_value_is_an_address_in_the_shared_heap,
		X_0_cannot_be_reassigned_Colon_a_closure_captures_it_and_its_value_is_a_counted_address_in_the_shared_heap,
		A_value_of_type_0_cannot_be_stored_in_an_ordinary_array_or_object_Colon_reference_counting_cannot_see_it_there_Use_a_SharedArray_or_a_shared_struct_field_or_a_place_typed_unknown_which_holds_a_handle,
		A_for_of_or_for_in_variable_cannot_have_type_0_Colon_its_value_is_an_address_in_the_shared_heap,
		X_0_cannot_be_captured_by_a_method_or_a_class_Colon_its_value_is_a_counted_address_in_the_shared_heap_which_only_functions_and_arrow_functions_can_hold,
		X_0_cannot_be_bound_by_destructuring_Colon_its_value_is_an_address_in_the_shared_heap,
		A_generator_s_parameter_cannot_have_type_0_Colon_the_generator_runs_after_its_caller_s_borrow_ends,
		Field_0_is_at_different_places_in_the_shared_structs_of_this_union,
		A_string_or_a_channel_in_a_shared_struct_or_SharedArray_cannot_be_assigned_by_destructuring_or_as_a_loop_variable,
		An_atomic_operation_takes_an_i32_field_of_a_shared_struct_named_by_a_string_literal_or_an_element_of_a_SharedArray_i32,
		This_value_may_not_be_an_integer_which_0_would_truncate_silently_Colon_convert_it_explicitly_as_with_Math_round_Math_trunc_or_0,
		X_0_blocks_its_thread_and_with_it_every_async_function_waiting_on_the_same_worker_Colon_in_an_async_function_use_1,
		X_0_blocks_its_thread_and_with_it_every_async_function_waiting_on_the_same_worker_which_an_async_function_must_not_do,
	} {
		if m.key == key {
			return m
		}
	}
	return nil
}
